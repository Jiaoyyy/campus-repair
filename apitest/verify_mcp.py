"""Live MCP protocol smoke test. Reads local .env; never prints credentials or tool content."""
import base64
import hashlib
import hmac
import json
import os
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

env = dict(line.split('=', 1) for line in (Path(__file__).resolve().parents[1] / '.env').read_text().splitlines() if '=' in line and not line.startswith('#'))
env.update({k: v for k, v in os.environ.items() if k in ('API_TEST_URL', 'MCP_TEST_URL')})
api = env.get('API_TEST_URL', 'http://127.0.0.1:' + env.get('WEB_PORT', '18083'))
mcp = env.get('MCP_TEST_URL', 'http://mcp:8081/mcp')
count = 0

def call(url, body, token=None, headers=None):
    h = {'Content-Type': 'application/json', **(headers or {})}
    if token:
        h['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(url, data=json.dumps(body).encode() if body is not None else None, headers=h, method='POST' if body is not None else 'GET')
    try:
        with urllib.request.urlopen(request, timeout=12) as response:
            data = response.read()
            return response.status, json.loads(data) if data else {}, response.headers
    except urllib.error.HTTPError as response:
        return response.code, {}, response.headers

def require(ok, label):
    global count
    assert ok, label
    count += 1

def login(name, password_var):
    status, body, _ = call(api + '/api/login', {'username': name, 'password': env[password_var]})
    require(status == 200, 'login ' + name)
    return body['access_token']

def signed(token, agent_id, request_id, offset=0):
    session_hash = hmac.new(env['SESSION_SECRET'].encode(), token.encode(), hashlib.sha256).hexdigest()
    now = int(time.time()) + offset
    claims = {'session_id': session_hash, 'agent_id': agent_id, 'request_id': request_id, 'audience': 'campus-repair-mcp', 'iat': now, 'exp': now + 60}
    payload = base64.urlsafe_b64encode(json.dumps(claims, separators=(',', ':')).encode()).decode().rstrip('=')
    signature = base64.urlsafe_b64encode(hmac.new(env['MCP_CALL_SECRET'].encode(), payload.encode(), hashlib.sha256).digest()).decode().rstrip('=')
    return payload + '.' + signature

admin = login('admin', 'SEED_ADMIN_PASSWORD')
requester = login('requester', 'SEED_REQUESTER_PASSWORD')
tech = login('tech', 'SEED_TECH_PASSWORD')
other = login('other', 'SEED_OTHER_PASSWORD')
status, discovered, _ = call(api + '/api/mcp/tools', None, admin)
require(status == 200 and {x['name'] for x in discovered['items']} == {'get_my_work_orders', 'get_assigned_work_orders', 'list_dispatch_candidates'}, 'SDK discovery')
status, config, _ = call(api + '/api/agents?manage=1', None, admin)
require(status == 200, 'agent config')
agents = {a['role']: a for a in config['items']}
names = {'requester': 'get_my_work_orders', 'technician': 'get_assigned_work_orders', 'admin': 'list_dispatch_candidates'}
for role, agent in agents.items():
    agent['tool_names'] = [names[role]]
    req = urllib.request.Request(api + '/api/agents/' + str(agent['id']), data=json.dumps(agent).encode(), headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + admin}, method='PUT')
    with urllib.request.urlopen(req, timeout=12) as response:
        require(response.status == 200, 'bind ' + role)

def protocol(token, role):
    agent = agents[role]
    credential = signed(token, agent['id'], uuid.uuid4().hex)
    headers = {'Accept': 'application/json, text/event-stream', 'Authorization': 'Bearer ' + credential}
    def post(body):
        status, result, response = call(mcp, body, None, headers)
        return status, result, response
    status, init, response = post({'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'repair-verifier', 'version': '1'}}})
    require(status == 200 and 'protocolVersion' in init.get('result', {}), role + ' initialize')
    headers['Mcp-Protocol-Version'] = init['result']['protocolVersion']
    if response.get('Mcp-Session-Id'):
        headers['Mcp-Session-Id'] = response['Mcp-Session-Id']
    status, _, _ = post({'jsonrpc': '2.0', 'method': 'notifications/initialized'})
    require(status == 202, role + ' initialized')
    status, listing, _ = post({'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list', 'params': {}})
    require(status == 200 and len(listing.get('result', {}).get('tools', [])) == 3, role + ' tools/list')
    def tool(name, args):
        return post({'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call', 'params': {'name': name, 'arguments': args, '_meta': {'campus_repair_auth': credential}}})
    args = {'category': 'lighting'} if role == 'admin' else {}
    status, result, _ = tool(names[role], args)
    require(status == 200 and not result.get('result', {}).get('isError') and 'items' in result.get('result', {}).get('structuredContent', {}), role + ' authorized call')
    items = result['result']['structuredContent']['items']
    allowed_fields = {'id', 'display_name', 'categories', 'pending_count', 'in_progress_count'} if role == 'admin' else {'id', 'number', 'status', 'category', 'location', 'created_at', 'updated_at'}
    require(all(set(item) <= allowed_fields for item in items), role + ' result has no extra private fields')
    if role in ('requester', 'technician'):
        status, visible, _ = call(api + '/api/work-orders?page_size=20', None, token)
        require(status == 200 and [x['id'] for x in items] == [x['id'] for x in visible['items']], role + ' matches work-order page')
    else:
        status, visible, _ = call(api + '/api/technicians?category=lighting', None, token)
        require(status == 200 and {x['id']: x['pending_count'] for x in items} == {x['id']: x['pending_count'] for x in visible['items']}, 'candidates match page')
    if role == 'requester':
        status, result, _ = tool('delete_work_order', {})
        require(status == 200 and 'error' in result, 'unknown write tool denied')
        status, result, _ = tool('list_dispatch_candidates', {'category': 'lighting'})
        require(status == 200 and result.get('result', {}).get('isError') is True, 'requester admin tool denied')
        status, result, _ = tool('get_my_work_orders', {'user_id': 1})
        require(status == 200 and (result.get('result', {}).get('isError') is True or 'error' in result), 'forged identity denied')
        status, result, _ = tool('get_my_work_orders', {})
        require(status == 200 and not result.get('result', {}).get('isError'), 'third authorized call')
        status, result, _ = tool('get_my_work_orders', {})
        require(status == 200 and result.get('result', {}).get('isError') is True, 'fourth call denied')

for token, role in [(requester, 'requester'), (tech, 'technician'), (admin, 'admin')]:
    protocol(token, role)
status, created, _ = call(api + '/api/conversations', {'agent_id': agents['requester']['id']}, requester)
require(status == 201, 'assistant conversation')
conversation_id = created['conversation']['id']
stream_request = urllib.request.Request(api + f'/api/conversations/{conversation_id}/messages', data=json.dumps({'content': '我的工单进度', 'mode': 'keyword'}).encode(), headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + requester}, method='POST')
with urllib.request.urlopen(stream_request, timeout=15) as response:
    events = response.read().decode()
require(response.status == 200 and 'event: meta' in events and 'event: delta' in events and 'event: done' in events and 'event: error' not in events, 'assistant uses MCP and completes SSE')
status, history, _ = call(api + f'/api/conversations/{conversation_id}/messages', None, requester)
require(status == 200 and any(x['role'] == 'assistant' and x['status'] == 'complete' for x in history['items']), 'assistant MCP result persisted')
status, _, _ = call(mcp, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'verifier', 'version': '1'}}}, signed(other, agents['requester']['id'], uuid.uuid4().hex), {'Accept': 'application/json, text/event-stream'})
require(status == 401, 'cross-org agent rejected')
status, _, _ = call(mcp, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'verifier', 'version': '1'}}}, signed(requester, agents['requester']['id'], uuid.uuid4().hex, -120), {'Accept': 'application/json, text/event-stream'})
require(status == 401, 'expired credential rejected')
temporary = login('requester', 'SEED_REQUESTER_PASSWORD')
stale = signed(temporary, agents['requester']['id'], uuid.uuid4().hex)
status, _, _ = call(api + '/api/logout', {}, temporary)
require(status == 200, 'logout')
status, _, _ = call(mcp, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'verifier', 'version': '1'}}}, stale, {'Accept': 'application/json, text/event-stream'})
require(status == 401, 'logged-out credential rejected')
status, config, _ = call(api + '/api/agents?manage=1', None, admin)
require(status == 200, 'refresh binding version')
requester_agent = next(a for a in config['items'] if a['role'] == 'requester')
requester_agent['tool_names'] = []
def put_agent(agent):
    req = urllib.request.Request(api + '/api/agents/' + str(agent['id']), data=json.dumps(agent).encode(), headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + admin}, method='PUT')
    with urllib.request.urlopen(req, timeout=12) as response:
        return response.status, json.load(response)['agent']
status, requester_agent = put_agent(requester_agent)
require(status == 200, 'disable binding')
try:
    credential = signed(requester, requester_agent['id'], uuid.uuid4().hex)
    headers = {'Accept': 'application/json, text/event-stream', 'Authorization': 'Bearer ' + credential}
    status, init, response = call(mcp, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'verifier', 'version': '1'}}}, None, headers)
    require(status == 200, 'disabled binding initialize')
    headers['Mcp-Protocol-Version'] = init['result']['protocolVersion']
    if response.get('Mcp-Session-Id'): headers['Mcp-Session-Id'] = response['Mcp-Session-Id']
    call(mcp, {'jsonrpc': '2.0', 'method': 'notifications/initialized'}, None, headers)
    status, denied, _ = call(mcp, {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/call', 'params': {'name': 'get_my_work_orders', 'arguments': {}, '_meta': {'campus_repair_auth': credential}}}, None, headers)
    require(status == 200 and denied.get('result', {}).get('isError') is True, 'disabled binding denied')
finally:
    requester_agent['tool_names'] = ['get_my_work_orders']
    status, _ = put_agent(requester_agent)
    require(status == 200, 'restore binding')
status, audit, _ = call(api + '/api/audit?page_size=100', None, admin)
outcomes = {x['outcome'] for x in audit.get('items', []) if x['action'] == 'mcp.call'}
require(status == 200 and {'success', 'denied'} <= outcomes, 'tool success and denial audited')
print(f'MCP protocol verification passed: {count} checks')
