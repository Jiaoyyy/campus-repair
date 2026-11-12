"""Mock-only building/date regression against isolated fictional old orders."""
import json
import subprocess
import urllib.request
import uuid
from pathlib import Path

root = Path(__file__).resolve().parents[1]
env = dict(line.split('=', 1) for line in (root/'tmp/draft-check.env').read_text().splitlines() if '=' in line and not line.startswith('#'))
base = 'http://127.0.0.1:' + env['WEB_PORT']
compose = ['docker', 'compose', '-p', 'campus-repair-draft-check', '--env-file', 'tmp/draft-check.env', '-f', 'compose.yaml', '-f', 'compose.verify.yaml', '-f', 'compose.knowledge-check.yaml', '-f', 'compose.draft-check.yaml']
cid = subprocess.check_output(compose + ['ps', '-q', 'api'], text=True).strip()
runtime = json.loads(subprocess.check_output(['docker', 'inspect', cid]))[0]
runtime_env = dict(item.split('=', 1) for item in runtime['Config']['Env'])
if runtime_env.get('CHAT_BASE_URL') != 'http://chat-mock:8080/v1':
    raise SystemExit('Refusing non-mock environment')

checks = []
def check(value, label):
    if not value:
        raise AssertionError(label)
    checks.append(label)
    print('PASS ' + label, flush=True)

def req(method, path, token=None, body=None):
    headers = {'Content-Type': 'application/json', 'Idempotency-Key': uuid.uuid4().hex}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    data = json.dumps(body).encode() if body is not None else None
    with urllib.request.urlopen(urllib.request.Request(base + path, method=method, headers=headers, data=data), timeout=20) as response:
        return json.load(response)

def sql(query):
    result = subprocess.run(compose + ['exec', '-T', 'db', 'sh', '-c', 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -N -B'], input=query, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError('Isolated fixture SQL failed; details suppressed')
    return result.stdout.strip()

tokens = {}
for name, key in [('admin', 'ADMIN'), ('requester', 'REQUESTER'), ('requester2', 'REQUESTER2'), ('other', 'OTHER'), ('tech', 'TECH')]:
    tokens[name] = req('POST', '/api/login', body={'username': name, 'password': env['SEED_' + key + '_PASSWORD']})['access_token']
agents = {name: req('GET', '/api/agents', token)['items'][0]['id'] for name, token in tokens.items()}
mode_path = root/'tmp/draft-mock/chat-mode'
original_mode = mode_path.read_bytes() if mode_path.exists() else None
snapshot = req('GET', '/api/agents?manage=1', tokens['admin'])['items']

def query(name, text, conversation=None):
    token = tokens[name]
    conversation = conversation or req('POST', '/api/conversations', token, {'agent_id': agents[name]})['conversation']['id']
    events, kind, parts = [], '', []
    request = urllib.request.Request(base + f'/api/conversations/{conversation}/messages', data=json.dumps({'content': text}).encode(), headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
    with urllib.request.urlopen(request, timeout=20) as response:
        for raw in response:
            line = raw.decode().strip()
            if line.startswith('event:'):
                kind = line[6:].strip()
            elif line.startswith('data:'):
                parts.append(line[5:].strip())
            elif not line and kind:
                events.append((kind, json.loads(''.join(parts))))
                kind, parts = '', []
    check(events[-1][0] == 'done' and sum(k in ('done', 'error') for k, _ in events) == 1, name + ' successful unique terminal')
    return conversation, events[-1][1]

try:
    for agent in snapshot:
        if 'query_work_orders' not in agent['tool_names']:
            req('PUT', f"/api/agents/{agent['id']}", tokens['admin'], {**agent, 'tool_names': agent['tool_names'] + ['query_work_orders']})
    mode_path.write_text('plan-building')
    orders = []
    for name, floor in [('requester', '一'), ('requester', '二'), ('requester2', '一'), ('other', '一'), ('requester', '二')]:
        order = req('POST', '/api/work-orders', tokens[name], {'category': 'lighting', 'location': '范围验收楼' + floor + '楼', 'description': '虚构旧工单灯不亮'})['order']
        orders.append(order)
        sql(f"UPDATE work_orders SET created_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 14 DAY) WHERE id={int(order['id'])}")
    sql(f"UPDATE work_orders SET status='closed' WHERE id={int(orders[-1]['id'])}")
    before = sql('SELECT CONCAT(COUNT(*),\":\",SUM(version),\":\",(SELECT COUNT(*) FROM work_order_events)) FROM work_orders')
    _, whole = query('admin', '范围验收楼有什么问题')
    check(whole['business']['query']['location'] == '范围验收楼' and whole['business']['query']['period'] == 'all', 'whole building has no invented date')
    check(not whole['business']['query'].get('keyword') and '不限日期' in whole['answer'], 'generic problem is not a symptom filter; scope displayed')
    whole_ids = {item['id'] for item in whole['business']['items']}
    check(all(order['id'] in whole_ids for order in (orders[0], orders[1], orders[2], orders[4])) and orders[3]['id'] not in whole_ids, 'old floors included with organization isolation')
    _, floor = query('admin', '范围验收楼一楼怎么了')
    check(floor['business']['query']['location'] == '范围验收楼一楼' and floor['business']['query']['period'] == 'all', 'floor is narrower but has same date semantics')
    check(all('一楼' in item['location'] for item in floor['business']['items']), 'floor does not include second floor')
    recent_cid, recent = query('admin', '范围验收楼最近有什么故障')
    check(recent['business']['query']['period'] == 'recent' and recent['business']['total'] == 0, 'explicit recent excludes fourteen-day-old creation')
    check('时间范围外仍可能有未结束工单' in recent['answer'], 'recent empty result explains older unresolved possibility')
    _, follow = query('admin', '那范围验收楼一楼呢', recent_cid)
    check(follow['business']['query']['period'] == 'recent' and follow['business']['total'] == 0, 'short followup keeps recent dates')
    _, reset = query('admin', '范围验收楼有什么问题', recent_cid)
    check(reset['business']['query']['period'] == 'all' and reset['business']['total'] > 0, 'new independent question clears old date scope')
    _, active = query('admin', '范围验收楼有哪些未解决问题')
    check(active['business']['query']['queue'] == 'active' and orders[-1]['id'] not in {item['id'] for item in active['business']['items']}, 'unresolved includes old orders and excludes closed')
    for name, expected in [('requester', {orders[0]['id'], orders[1]['id'], orders[4]['id']}), ('requester2', {orders[2]['id']}), ('other', {orders[3]['id']}), ('tech', set())]:
        _, result = query(name, '范围验收楼有什么问题')
        visible = {item['id'] for item in result['business']['items']}
        check(expected.issubset(visible) and not (set(order['id'] for order in orders) - expected) & visible, name + ' ownership/assignment isolation')
    check(before == sql('SELECT CONCAT(COUNT(*),\":\",SUM(version),\":\",(SELECT COUNT(*) FROM work_order_events)) FROM work_orders'), 'all queries leave business unchanged')
finally:
    if original_mode is None:
        mode_path.unlink(missing_ok=True)
    else:
        mode_path.write_bytes(original_mode)
    current = req('GET', '/api/agents?manage=1', tokens['admin'])['items']
    for agent in snapshot:
        latest = next(item for item in current if item['id'] == agent['id'])
        if latest['tool_names'] != agent['tool_names']:
            req('PUT', f"/api/agents/{agent['id']}", tokens['admin'], {**latest, 'tool_names': agent['tool_names']})
(root/'tmp/building-query-results.json').write_text(json.dumps({'gateway': 'mock', 'checks': checks}, ensure_ascii=False, indent=2) + '\n')
print(f'{len(checks)} building query checks passed; mock only')
