"""Extended MCP protocol checks. Run inside the dedicated mock Docker network.
Mount its credentials.json at /verify/credentials.json. No response data printed.
"""
import base64
import datetime as dt
import hashlib
import hmac
import json
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

credentials = json.loads(Path('/verify/credentials.json').read_text())
API = 'http://api:8080'
MCP = 'http://mcp:8081/mcp'
count = 0


def require(value, label):
    global count
    if not value:
        raise AssertionError(label)
    count += 1
    print('PASS ' + label, flush=True)


def http(method, url, body=None, token=None, headers=None, expected=200):
    hs = {'Content-Type': 'application/json', **(headers or {})}
    if token:
        hs['Authorization'] = 'Bearer ' + token
    raw = body if isinstance(body, bytes) else json.dumps(body).encode() if body is not None else None
    try:
        with urllib.request.urlopen(urllib.request.Request(url, method=method, headers=hs, data=raw), timeout=15) as response:
            status, data, response_headers = response.status, response.read(), response.headers
    except urllib.error.HTTPError as response:
        status, data, response_headers = response.code, response.read(), response.headers
    require(status == expected, method + ' ' + url.split('/')[-1] + ' HTTP ' + str(expected))
    return json.loads(data) if data and status < 400 else {}, response_headers


def api(method, path, token=None, body=None, expected=200, headers=None):
    return http(method, API + path, body, token, headers, expected)[0]


def signature(token, agent_id):
    now = int(time.time())
    session = hmac.new(credentials['SESSION_SECRET'].encode(), token.encode(), hashlib.sha256).hexdigest()
    claim = {'session_id': session, 'agent_id': agent_id, 'request_id': uuid.uuid4().hex, 'audience': 'campus-repair-mcp', 'iat': now, 'exp': now + 60}
    payload = base64.urlsafe_b64encode(json.dumps(claim).encode()).decode().rstrip('=')
    digest = base64.urlsafe_b64encode(hmac.new(credentials['MCP_CALL_SECRET'].encode(), payload.encode(), hashlib.sha256).digest()).decode().rstrip('=')
    return payload + '.' + digest


def tool(token, agent, name, arguments, success=True, authentication_denied=False):
    credential = signature(token, agent['id'])
    headers = {'Accept': 'application/json, text/event-stream', 'Authorization': 'Bearer ' + credential}
    init, response = http('POST', MCP, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'extensions-check', 'version': '1'}}}, headers=headers, expected=401 if authentication_denied else 200)
    if authentication_denied:
        require(not success, 'cross-organization caller refused at initialization')
        return None
    headers['Mcp-Protocol-Version'] = init['result']['protocolVersion']
    headers['Mcp-Session-Id'] = response['Mcp-Session-Id']
    http('POST', MCP, {'jsonrpc': '2.0', 'method': 'notifications/initialized'}, headers=headers, expected=202)
    result, _ = http('POST', MCP, {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/call', 'params': {'name': name, 'arguments': arguments, '_meta': {'campus_repair_auth': credential}}}, headers=headers)
    valid = 'error' not in result and not result.get('result', {}).get('isError')
    require(valid == success, name + (' allowed' if success else ' refused'))
    return result.get('result', {}).get('structuredContent')


tokens, users = {}, {}
for user, key in (('admin', 'ADMIN'), ('requester', 'REQUESTER'), ('requester2', 'REQUESTER2'), ('tech', 'TECH'), ('tech2', 'TECH2'), ('other', 'OTHER')):
    login = api('POST', '/api/login', body={'username': user, 'password': credentials['SEED_' + key + '_PASSWORD']})
    tokens[user], users[user] = login['access_token'], login['user']
admin = tokens['admin']
snapshot = api('GET', '/api/agents?manage=1', admin)['items']
agents = {a['role']: a for a in snapshot}
extra = ['get_work_order_detail', 'get_work_order_visit', 'search_repair_knowledge']
listing = api('GET', '/api/mcp/tools', admin)['items']
require(len(listing) == 7 and set(extra).issubset(x['name'] for x in listing), 'seven real discovered tools')
try:
    order = api('POST', '/api/work-orders', tokens['requester'], {'category': 'lighting', 'location': '虚构工具验收楼201', 'description': '灯不亮'}, 201, {'Idempotency-Key': str(uuid.uuid4())})['order']
    # New tools are discovered but never automatically bound on upgrade.
    tool(tokens['requester'], agents['requester'], extra[0], {'number': order['number']}, False)
    for old in snapshot:
        payload = {**old, 'tool_names': list(dict.fromkeys(old.get('tool_names', []) + extra + ['query_work_orders']))}
        agents[old['role']] = api('PUT', '/api/agents/' + str(old['id']), admin, payload)['agent']
    def action(user, name, **fields):
        global order
        order = api('POST', '/api/work-orders/' + str(order['id']) + '/' + name, tokens[user], {'version': order['version'], **fields})['order']
    action('admin', 'review', decision='approve')
    action('admin', 'assign', technician_id=users['tech']['id'])
    for user, role in (('requester', 'requester'), ('admin', 'admin'), ('tech', 'technician')):
        detail = tool(tokens[user], agents[role], extra[0], {'number': order['number'], 'include_history': True, 'limit': 1})['detail']
        require(detail['order']['number'] == order['number'] and detail['version'] == order['version'], 'detail matches normal business API')
        require(detail['has_more'] and detail['next_cursor'], 'bounded history cursor')
        second = tool(tokens[user], agents[role], extra[0], {'number': order['number'], 'include_history': True, 'cursor': detail['next_cursor'], 'limit': 1})['detail']
        require(second['events'][0]['id'] < detail['events'][0]['id'], 'history stable descending pagination')
    tool(tokens['requester2'], agents['requester'], extra[0], {'number': order['number']}, False)
    tool(tokens['other'], agents['requester'], extra[0], {'number': order['number']}, False, authentication_denied=True)
    tool(tokens['requester'], agents['requester'], extra[0], {'number': 'RP000000000000'}, False)
    tool(tokens['requester'], agents['requester'], extra[0], {'number': order['number'], 'include_history': True, 'cursor': 'invalid'}, False)
    tool(tokens['requester'], agents['requester'], extra[0], {'number': order['number'], 'org_id': 2}, False)
    tool(tokens['requester'], agents['requester'], extra[0], {'number': order['number'], 'limit': 51}, False)
    action('admin', 'reassign', technician_id=users['tech2']['id'], reason='虚构改派验收')
    tool(tokens['tech'], agents['technician'], extra[0], {'number': order['number']}, False)
    action('tech2', 'accept')
    action('tech2', 'records', measures='检查灯具', result='记录故障待处理')
    detail = tool(tokens['tech2'], agents['technician'], extra[0], {'number': order['number']})['detail']
    require(detail['latest_repair']['measures'] == '检查灯具', 'latest repair measures preserved')
    visit = tool(tokens['requester'], agents['requester'], extra[1], {'number': order['number']})['detail']
    require(visit['visit']['availability_start'] is None and visit['visit']['visit'] is None, 'unscheduled visit distinct from confirmed appointment')
    query = tool(tokens['requester'], agents['requester'], 'query_work_orders', {'number': order['number'], 'include_next_steps': True})
    require(query['items'][0]['next_step']['evidence_id'].endswith('v' + str(order['version'])), 'next step linked to current order version')
    def visit_action(user, name, **fields):
        method='PUT' if name=='availability' else 'POST'
        path='/api/work-orders/'+str(order['id'])+'/visit/'+('availability' if name=='availability' else 'actions/'+name)
        result=api(method,path,tokens[user],{'version':order['version'],**fields})
        order['version']=result['version']
    start=dt.datetime.now(dt.timezone.utc)+dt.timedelta(hours=2)
    window={'start':start.isoformat(),'end':(start+dt.timedelta(hours=1)).isoformat()}
    visit_action('requester','availability',**window)
    visit_action('tech2','schedule',**window)
    scheduled=tool(tokens['requester'],agents['requester'],extra[1],{'number':order['number']})['detail']
    require(scheduled['visit']['visit']['stage']=='scheduled' and scheduled['visit']['availability_start'],'availability and appointment remain distinct')
    visit_action('tech2','depart')
    departed=tool(tokens['requester'],agents['requester'],extra[1],{'number':order['number']})['detail']
    require(departed['visit']['visit']['departed_at'] and '出发' in departed['next_step']['reason'],'departure is based on recorded timestamp')
    visit_action('tech2','arrive')
    visit_action('tech2','no-show',reason='虚构验收：到场无人')
    absent=tool(tokens['requester'],agents['requester'],extra[1],{'number':order['number']})['detail']
    require(absent['latest_visit']['no_show_at'] and absent['next_step']['responsible_role']=='requester','no-show evidence and requester next step')
    visit_action('tech2','wait-parts',reason='虚构验收：等待备件',expected_resume_at=(start+dt.timedelta(days=1)).isoformat())
    parts=tool(tokens['tech2'],agents['technician'],extra[1],{'number':order['number']})['detail']
    require(parts['visit']['exception']=='waiting_parts' and parts['next_step']['responsible_role']=='technician','parts takes precedence while preserving revisit requirement')
    visit_action('tech2','resume',reason='虚构验收：备件到齐')
    resumed=tool(tokens['requester'],agents['requester'],extra[1],{'number':order['number']})['detail']
    require(resumed['visit']['needs_availability'] and resumed['next_step']['responsible_role']=='requester','parts recovery preserves unresolved rescheduling')
    start=start+dt.timedelta(hours=2)
    window={'start':start.isoformat(),'end':(start+dt.timedelta(hours=1)).isoformat()}
    visit_action('requester','availability',reason='虚构验收：重新约时',**window)
    visit_action('tech2','schedule',**window)
    visit_action('tech2','depart')
    visit_action('tech2','arrive')
    action('tech2','complete',measures='虚构更换灯具',result='虚构修复完成')
    action('requester','confirm')
    terminal=tool(tokens['requester'],agents['requester'],extra[1],{'number':order['number']})['detail']
    require(terminal['order']['status']=='closed' and terminal['next_step']['actions']==[],'closed order has no executable next-step actions')
    location = '虚构分页验收' + uuid.uuid4().hex[:8]
    for _ in range(7):
        api('POST', '/api/work-orders', tokens['requester'], {'category': 'lighting', 'location': location, 'description': '虚构待确认的上门问题'}, 201, {'Idempotency-Key': str(uuid.uuid4())})
    criteria = {'analysis': 'overview', 'location': location, 'period': 'all', 'queue': 'active'}
    empty = tool(admin, agents['admin'], 'query_work_orders', criteria)
    require(empty['total'] == 7 and not empty['analysis'].get('related'), 'unchanged query shape and aggregate work without metric arguments')
    # This separate location is filled with aged work orders by verify_insights;
    # here a forged cursor is still rejected even before any matching row.
    tool(admin, agents['admin'], 'query_work_orders', {**criteria, 'metric': 'aged', 'metric_cursor': '1.' + '0'*64}, False)
    knowledge = tool(tokens['requester'], agents['requester'], extra[2], {'query': '安全资料'})
    require(knowledge['state'] == 'empty' and knowledge['items'] == [], 'no bound published material returns empty evidence')
    boundary = uuid.uuid4().hex
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n虚构安全工具资料\r\n--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="safety.txt"\r\nContent-Type: text/plain\r\n\r\n虚构安全资料：发现灯具异常应停止使用并联系维修人员。不要自行拆卸、接线或带电维修。\r\n--{boundary}--\r\n').encode()
    uploaded, _ = http('POST', API + '/api/materials', body, admin, {'Content-Type': 'multipart/form-data; boundary=' + boundary}, 201)
    material_id = uploaded['id']
    for _ in range(60):
        material = api('GET', '/api/materials/' + str(material_id), admin)['material']
        if material['generation'] > 0:
            break
        time.sleep(.2)
    require(material['generation'] > 0, 'real indexing worker publishes mock-embedded text')
    agent = agents['requester']
    agent = api('PUT', '/api/agents/' + str(agent['id']), admin, {**agent, 'material_ids': [material_id]})['agent']
    agents['requester'] = agent
    knowledge = tool(tokens['requester'], agent, extra[2], {'query': '灯具安全', 'limit': 5})
    require(knowledge['items'] and all(e['material_id'] == material_id and e['generation'] == material['generation'] for e in knowledge['items']), 'knowledge reuses bound current-generation RAG')
    removed = api('PUT', '/api/agents/' + str(agent['id']), admin, {**agent, 'material_ids': []})['agent']
    require(tool(tokens['requester'], removed, extra[2], {'query': '灯具安全'})['items'] == [], 'material unbinding takes effect')
finally:
    current = api('GET', '/api/agents?manage=1', admin)['items']
    for old in snapshot:
        live = next(a for a in current if a['id'] == old['id'])
        api('PUT', '/api/agents/' + str(old['id']), admin, {**old, 'version': live['version']})
print('PASS MCP extension checks ' + str(count))
