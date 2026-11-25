"""Audit, literal location and visit boundary checks against draft-check only."""
import argparse
import datetime as dt
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from zoneinfo import ZoneInfo

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--override', required=True)
parser.add_argument('--faults', action='store_true')
args = parser.parse_args()
root = Path(__file__).resolve().parents[1]
project = 'campus-repair-draft-check'
environment = root / 'tmp/draft-check.env'
config = dict(line.split('=', 1) for line in environment.read_text().splitlines() if '=' in line and not line.startswith('#'))
base = 'http://127.0.0.1:' + config['WEB_PORT']
compose = ['docker', 'compose', '-p', project, '--env-file', str(environment), '-f', 'compose.yaml', '-f',
           'compose.verify.yaml', '-f', 'compose.knowledge-check.yaml', '-f', 'compose.draft-check.yaml', '-f', args.override]


def dc(*command, input=None):
    return subprocess.run(compose + list(command), cwd=root, text=True, input=input,
                          check=True, capture_output=True).stdout.strip()


def check(value, description):
    if not value:
        raise AssertionError(description)
    print('PASS ' + description, flush=True)


def request(method, path, token=None, body=None, expected=200):
    headers = {'Content-Type': 'application/json', 'Idempotency-Key': uuid.uuid4().hex}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    try:
        with urllib.request.urlopen(urllib.request.Request(base + path, method=method, headers=headers,
                                    data=json.dumps(body).encode() if body is not None else None), timeout=20) as response:
            status, data, request_id = response.status, json.load(response), response.headers.get('X-Request-ID')
    except urllib.error.HTTPError as response:
        status, data, request_id = response.code, json.load(response), response.headers.get('X-Request-ID')
    if status != expected:
        raise AssertionError(f'{method} {path.partition("?")[0]}: HTTP {status}, expected {expected}')
    return data, request_id


api_id = dc('ps', '-q', 'api')
runtime = json.loads(subprocess.check_output(['docker', 'inspect', api_id]))[0]
env = dict(value.split('=', 1) for value in runtime['Config']['Env'])
assert runtime['Config']['Labels']['com.docker.compose.project'] == project
assert env['CHAT_BASE_URL'] == 'http://chat-mock:8080/v1'
assert all(m['Name'].startswith(project + '_') for m in runtime['Mounts'] if m['Type'] == 'volume')
assert env['MCP_SERVER_URL'] == ('http://mcp-fault:8081/mcp' if args.faults else 'http://mcp:8081/mcp')

tokens, users, login_requests = {}, {}, {}
for name, key in [('admin', 'ADMIN'), ('requester', 'REQUESTER'), ('tech', 'TECH'), ('other', 'OTHER'), ('other_admin', 'OTHER')]:
    data, login_requests[name] = request('POST', '/api/login', body={'username': name, 'password': config[f'SEED_{key}_PASSWORD']})
    tokens[name], users[name] = data['access_token'], data['user']
admin, requester = tokens['admin'], tokens['requester']


def audit(**filters):
    return request('GET', '/api/audit?' + urllib.parse.urlencode(filters), admin)[0]


if args.faults:
    agent = request('GET', '/api/agents', requester)[0]['items'][0]
    current = next(a for a in request('GET', '/api/agents?manage=1', admin)[0]['items'] if a['id'] == agent['id'])
    changed = {**current, 'tool_names': list(set(current['tool_names']) | {'query_work_orders'})}
    request('PUT', f'/api/agents/{agent["id"]}', admin, changed)
    try:
        for mode in ('malformed', 'timeout'):
            dc('exec', '-T', 'mcp-fault', 'sh', '-c', 'printf %s "$1" > /tmp/mcp-fault-mode', 'sh', mode)
            cid = request('POST', '/api/conversations', requester, {'agent_id': agent['id']}, 201)[0]['conversation']['id']
            post = urllib.request.Request(base+f'/api/conversations/{cid}/messages', method='POST',
                   headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + requester},
                   data=json.dumps({'content': '我的报修还有多少单未结束？'}, ensure_ascii=False).encode())
            started = time.monotonic()
            with urllib.request.urlopen(post, timeout=20) as response:
                rid = response.headers['X-Request-ID']
                packet = response.read().decode()
            elapsed = time.monotonic() - started
            check('event: error' in packet and 'tool_unavailable' in packet and 'event: done' not in packet
                  and 'event: delta' not in packet, f'{mode}: no business answer on failed MCP call')
            rows = audit(request_id=rid, action='mcp.host', outcome='failed')['items']
            check(len(rows) == 1 and rows[0]['actor_id'] == users['requester']['id'], f'{mode}: Host failure is attributable')
            expected_reason = 'timeout' if mode == 'timeout' else 'transport_or_protocol'
            check(rows[0]['metadata']['reason'] == expected_reason, f'{mode}: diagnostic reason is recorded')
            if mode == 'timeout':
                check(9.5 <= elapsed <= 12 and 9500 <= rows[0]['metadata']['duration_ms'] <= 11000,
                      f'10-second MCP deadline confirmed ({elapsed:.2f}s end to end)')
            request('GET', '/api/work-orders?page_size=1', requester)
    finally:
        current['version'] = next(a['version'] for a in request('GET', '/api/agents?manage=1', admin)[0]['items'] if a['id'] == agent['id'])
        request('PUT', f'/api/agents/{agent["id"]}', admin, current)
    raise SystemExit(0)

_, denial = request('GET', '/api/audit', requester, expected=403)
check(audit(request_id=login_requests['requester'], action='login', outcome='success')['total'] == 1, 'successful sign-in is traceable')
check(audit(request_id=denial, action='access.denied', actor_id=users['requester']['id'])['total'] == 1,
      'forbidden request is traceable')
fake_password = 'isolated-incorrect-' + uuid.uuid4().hex
_, failed_login = request('POST', '/api/login', body={'username': 'requester', 'password': fake_password}, expected=401)
assert len(failed_login) == 32 and all(c in '0123456789abcdef' for c in failed_login)
logged = dc('exec', '-T', 'db', 'sh', '-c', 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -N -B',
            input=f"SELECT CONCAT(action,':',outcome,':',actor_id IS NULL,':',org_id IS NULL) FROM audit_events WHERE request_id='{failed_login}'")
check(logged == 'login:denied:1:1', 'failed sign-in is recorded anonymously')
request('GET', '/api/work-orders/stats', requester, expected=403)
request('GET', '/api/work-orders/stats', tokens['tech'], expected=403)
prior = request('GET', '/api/work-orders/stats', admin)[0]
tag = '虚构审计' + uuid.uuid4().hex[:8]
def create(role, location, category='lighting'):
    return request('POST', '/api/work-orders', tokens[role],
                   {'category': category, 'location': location, 'description': '虚构审核夹具'}, 201)
created, rid = create('requester', tag + '%_')
order = created['order']
create('requester', tag + 'AA')
outside, other_rid = create('other', tag + '%_', 'plumbing')
outside_id = outside['order']['id']
assert outside_id != order['id']
after = request('GET', '/api/work-orders/stats', admin)[0]
check(after['total'] == prior['total'] + 2 and after['statuses']['submitted'] == prior['statuses'].get('submitted', 0) + 2
      and after['categories']['lighting'] == prior['categories'].get('lighting', 0) + 2
      and after['categories'].get('plumbing', 0) == prior['categories'].get('plumbing', 0),
      'organization status and category totals exclude the other organization')
check(sum(after['statuses'].values()) == sum(after['categories'].values()) == after['total'],
      'both breakdowns match the single aggregate total')
location = '/api/work-orders?location=' + urllib.parse.quote(tag + '%_')
check([o['id'] for o in request('GET', location, admin)[0]['items']] == [order['id']], 'location treats percent and underscore literally')
date = dt.datetime.now(ZoneInfo('Asia/Shanghai')).date().isoformat()
rows = audit(resource_id=order['id'], request_id=rid, action='work_order.*', outcome='success',
             actor_id=users['requester']['id'], date_from=date, date_to=date)
check(rows['total'] == 1 and rows['items'][0]['request_id'] == rid, 'all audit filters combine with inclusive Shanghai dates')
check(audit(request_id=other_rid)['total'] == 0, 'audit search cannot return another organization')
check(audit(request_id=rid, outcome='denied')['total'] == 0, 'outcome filter excludes successful action')
check(audit(request_id=rid, date_to=(dt.date.fromisoformat(date) - dt.timedelta(days=1)).isoformat())['total'] == 0,
      'audit date exclusion works')
for invalid in ('actor_id=0', 'action=work_order.%25', 'date_from=2026-01-02&date_to=2026-01-01'):
    request('GET', '/api/audit?' + invalid, admin, expected=400)
check(True, 'invalid audit filters rejected')

oid = order['id']
prefix = f'/api/work-orders/{oid}'
for role, action, fields in (
    ('admin', 'review', {'decision': 'approve'}),
    ('admin', 'assign', {'technician_id': users['tech']['id']}),
    ('tech', 'accept', {}),
):
    response, action_rid = request('POST', f'{prefix}/{action}', tokens[role], {'version': order['version'], **fields})
    order = response['order']
    check(audit(request_id=action_rid, action='work_order.'+action, resource_id=oid)['total'] == 1,
          f'{action} produces linked audit evidence')
now = dt.datetime.now(dt.timezone.utc)
window = {'start': (now + dt.timedelta(hours=1)).isoformat(), 'end': (now + dt.timedelta(hours=2)).isoformat()}
request('PUT', prefix+'/visit/availability', requester, {'version': order['version'], **window})
dc('exec', '-T', 'db', 'sh', '-c', 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE"',
   input=f"UPDATE work_order_visit_context SET availability_start=UTC_TIMESTAMP(6)-INTERVAL 2 HOUR,availability_end=UTC_TIMESTAMP(6)-INTERVAL 1 HOUR WHERE work_order_id={int(oid)}")
tech_visit = request('GET', prefix+'/visit', tokens['tech'])[0]
owner_visit = request('GET', prefix+'/visit', requester)[0]
requeue = request('GET', '/api/repair-workbench?view=reschedule&page_size=100', admin)[0]
check('schedule' not in tech_visit['allowed_actions'] and 'request-availability' not in tech_visit['allowed_actions']
      and 'availability' in owner_visit['allowed_actions'] and not tech_visit['context']['needs_availability']
      and all(item.get('id', item.get('order', {}).get('id')) != oid for item in requeue['items']),
      'expired unbooked availability: requester can update, technician cannot request a new window, admin queue omits it')


agent = request('GET', '/api/agents', requester)[0]['items'][0]
config_before = next(a for a in request('GET', '/api/agents?manage=1', admin)[0]['items'] if a['id'] == agent['id'])
_, config_rid = request('PUT', f'/api/agents/{agent["id"]}', admin, config_before)
check(audit(request_id=config_rid, action='agent.update', outcome='success')['total'] == 1, 'assistant configuration update is traceable')
cid = request('POST', '/api/conversations', requester, {'agent_id': agent['id']}, 201)[0]['conversation']['id']
dialogue = '三号楼201灯坏了，帮我报修。'+tag
post = urllib.request.Request(base+f'/api/conversations/{cid}/messages', method='POST',
       headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + requester},
       data=json.dumps({'content': dialogue}, ensure_ascii=False).encode())
with urllib.request.urlopen(post, timeout=20) as response:
    rid = response.headers['X-Request-ID']
    packet = response.read().decode()
check('event: done' in packet and '"draft"' in packet, 'fixture obtains draft without confirmation')
traces = audit(request_id=rid, page_size=100)['items']
check({'conversation.generate', 'knowledge.search', 'skill.execute', 'draft.generate', 'conversation.finish', 'assistant.performance'}
      <= {row['action'] for row in traces}, 'same request traces retrieval, skill, draft and outcome')
inspected = json.dumps(traces, ensure_ascii=False) + dc('logs', '--tail', '40', 'api', 'mcp')
check(all(secret not in inspected for secret in (dialogue, fake_password, requester, config['SEED_REQUESTER_PASSWORD'])),
      'audit and service logs omit sampled conversation and credential values')
