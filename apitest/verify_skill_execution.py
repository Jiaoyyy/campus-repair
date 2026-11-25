"""End-to-end skill/answer checks in the fixed isolated mock environment."""
import concurrent.futures
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
HOME = ROOT / 'tmp/tools-skills-check'
BASE = 'http://127.0.0.1:18090'
DC = ['docker', 'compose', '-p', 'campus-repair-tools-skills-check', '--project-directory', str(ROOT), '-f', str(HOME / 'compose.json')]
checks = 0


def check(value, label):
    global checks
    if not value:
        raise AssertionError(label)
    checks += 1
    print('PASS ' + label, flush=True)


def api(method, path, token=None, body=None, expected=200, headers=None):
    hs = {'Content-Type': 'application/json', **(headers or {})}
    if token:
        hs['Authorization'] = 'Bearer ' + token
    data = json.dumps(body).encode() if body is not None else None
    try:
        with urllib.request.urlopen(urllib.request.Request(BASE + path, method=method, headers=hs, data=data), timeout=15) as response:
            status, value = response.status, json.load(response)
    except urllib.error.HTTPError as response:
        status, value = response.code, json.load(response)
    check(status == expected, method + ' ' + path + ' HTTP ' + str(expected))
    return value


def mode(value):
    subprocess.run(DC + ['exec', '-T', 'chat-mock', 'python', '-c',
                        'import sys; from pathlib import Path; Path("/state/chat-mode").write_text(sys.argv[1])', value],
                   check=True, capture_output=True)


def requests():
    path = HOME / 'state/chat-requests.jsonl'
    return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []


def conversation(user):
    return api('POST', '/api/conversations', tokens[user], {'agent_id': agents[roles[user]]['id']}, 201)['conversation']['id']


def turn(user, cid, question, success=True):
    req = urllib.request.Request(BASE + f'/api/conversations/{cid}/messages', method='POST', headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + tokens[user]}, data=json.dumps({'content': question}).encode())
    with urllib.request.urlopen(req, timeout=20) as response:
        raw = response.read().decode()
    events = []
    for block in raw.split('\n\n'):
        event, data = '', []
        for line in block.splitlines():
            if line.startswith('event:'):
                event = line[6:].strip()
            if line.startswith('data:'):
                data.append(line[5:].strip())
        if event:
            events.append((event, json.loads(''.join(data))))
    check(events[0][0] == 'meta', 'meta emitted first')
    check(sum(e in ('done', 'error') for e, _ in events) == 1, 'single terminal event')
    check(events[-1][0] == ('done' if success else 'error'), 'expected turn outcome')
    return events


container = json.loads(subprocess.check_output(['docker', 'inspect', 'campus-repair-tools-skills-check-api-1']))[0]
runtime = dict(value.split('=', 1) for value in container['Config']['Env'])
check(runtime['CHAT_BASE_URL'] == 'http://chat-mock:8080/v1', 'actual mock gateway')
credentials = json.loads((HOME / 'credentials.json').read_text())
tokens, users = {}, {}
roles = {'admin': 'admin', 'requester': 'requester', 'tech': 'technician'}
for name, key in (('admin', 'ADMIN'), ('requester', 'REQUESTER'), ('tech', 'TECH')):
    value = api('POST', '/api/login', body={'username': name, 'password': credentials['SEED_' + key + '_PASSWORD']})
    tokens[name], users[name] = value['access_token'], value['user']
admin = tokens['admin']
snapshot = api('GET', '/api/agents?manage=1', admin)['items']
original_skills = api('GET', '/api/skills', admin)['items']
agents = {}
marker = '角色请求验收' + uuid.uuid4().hex[:8]
materials = api('GET', '/api/materials?page_size=100', admin)['items']
ready_materials = [m['id'] for m in materials if m['generation'] > 0]
check(bool(ready_materials), 'prepared fictional knowledge')
try:
    found = api('GET', '/api/mcp/tools', admin)['items']
    for old in snapshot:
        tools = [t['name'] for t in found if t['role'] in ('all', old['role'])]
        bindings = [{'skill_id': s['id'], 'enabled': True} for s in original_skills if old['role'] in s['roles']]
        agents[old['role']] = api('PUT', '/api/agents/' + str(old['id']), admin, {**old, 'tool_names': tools, 'skill_bindings': bindings, 'material_ids': ready_materials, 'prompt': marker + old['role']})['agent']
    for skill in original_skills:
        if not skill['enabled']:
            api('PUT', '/api/skills/' + skill['id'], admin, {'version': skill['version'], 'enabled': True})
    mode('ok')
    order = api('POST', '/api/work-orders', tokens['requester'], {'category': 'lighting', 'location': '虚构执行验收楼201', 'description': '灯不亮'}, 201, {'Idempotency-Key': str(uuid.uuid4())})['order']
    cid = conversation('requester')
    start = len(requests())
    done = turn('requester', cid, order['number'] + '为什么还没修好？')[-1][1]
    check(done['business']['detail']['order']['id'] == order['id'], 'normal chat reaches detail MCP tool')
    check(done['business']['natural'] and '没有说明延期原因' in done['answer'], 'unrecorded reason answered directly')
    new_requests = requests()[start:]
    check(any('REPAIR_NATURAL_ANSWER_V1' in r['messages'][0]['content'] and marker in str(r['messages']) for r in new_requests), 'role prompt actually passed to business-answer gateway')
    done = turn('requester', conversation('requester'), order['number'] + '上门预约是什么时候？')[-1][1]
    check(done['business']['kind'] == 'visit' and done['business']['detail']['visit']['visit'] is None, 'visit tool preserves missing appointment')
    safety_cid = conversation('requester')
    mode('safe-warning')
    done = turn('requester', safety_cid, '维修时有哪些安全注意事项？')[-1][1]
    check(done['skill']['skill_id'] == 'safe_self_check' and done['citations'], 'safety skill has actual citations')
    history = api('GET', f'/api/conversations/{safety_cid}/messages', tokens['requester'])['items']
    check(history[-1]['skill']['skill_id'] == 'safe_self_check', 'actual skill persisted in history')
    check(any(marker in str(r['messages']) and ('本轮业务技能：安全自查建议，定义版本'+str(next(s['definition_version'] for s in original_skills if s['id']=='safe_self_check'))) in str(r['messages']) for r in requests()), 'role plus versioned skill reached gateway')
    semantic=turn('requester',conversation('requester'),'灯坏了我可以自己处理吗')[-1][1]
    check(semantic.get('skill',{}).get('skill_id')=='safe_self_check','native planner routes a paraphrased safety skill')
    mode('ok')
    cid = conversation('requester')
    draft = turn('requester', cid, '三号楼201，灯不亮，帮我报修')[-1][1]
    check(draft['skill']['skill_id'] == 'repair_intake' and draft['draft']['status'] == 'active', 'intake produces an unsubmitted private draft')
    fields = {k: draft['draft'][k] for k in ('category', 'location', 'description')}
    fields.update(draft_message_id=draft['draft']['message_id'], draft_version=draft['draft']['version'])
    key = draft['draft']['idempotency_key']
    created = api('POST', '/api/work-orders', tokens['requester'], fields, 201, {'Idempotency-Key': key})
    replay = api('POST', '/api/work-orders', tokens['requester'], fields, 200, {'Idempotency-Key': key})
    check(created['order']['id'] == replay['order']['id'], 'explicit confirmation retry creates one order')
    done = turn('admin', conversation('admin'), '照明工单推荐安排谁？')[-1][1]
    check(done['skill']['skill_id'] == 'dispatch_advice' and done['business']['candidates'], 'administrator dispatch skill uses actual candidates')
    check(any('REPAIR_NATURAL_ANSWER_V1' in r['messages'][0]['content'] and '派单辅助' in str(r['messages']) and marker + 'admin' in str(r['messages']) for r in requests()), 'dispatch role and skill instructions sent to answer gateway')
    revoked_order=api('POST','/api/work-orders',tokens['requester'],{'category':'lighting','location':'虚构撤权验收楼201','description':'灯不亮'},201,{'Idempotency-Key':str(uuid.uuid4())})['order']
    revoked_order=api('POST','/api/work-orders/'+str(revoked_order['id'])+'/review',admin,{'version':revoked_order['version'],'decision':'approve'})['order']
    revoked_order=api('POST','/api/work-orders/'+str(revoked_order['id'])+'/assign',admin,{'version':revoked_order['version'],'technician_id':users['tech']['id']})['order']
    tech_cid=conversation('tech')
    old_detail=turn('tech',tech_cid,revoked_order['number']+'进度是什么？')[-1][1]
    alternate=next(t for t in api('GET','/api/technicians?category=lighting',admin)['items'] if t['id']!=users['tech']['id'])
    api('POST','/api/work-orders/'+str(revoked_order['id'])+'/reassign',admin,{'version':revoked_order['version'],'technician_id':alternate['id'],'reason':'虚构权限复核'})
    history=api('GET',f'/api/conversations/{tech_cid}/messages',tokens['tech'])['items']
    check(history[-1]['business'].get('unavailable') and not history[-1]['business'].get('detail') and history[-1]['business']['statuses']=={},'revoked detail history redacts payload and prose')
    start=len(requests())
    turn('tech',tech_cid,'你好')
    check(all(m.get('content')!=old_detail['answer'] for r in requests()[start:] for m in r['messages'] if m.get('role')=='assistant'),'revoked business prose is not sent back to gateway')
    mode('answer-forged')
    start = len(requests())
    forged = turn('requester', conversation('requester'), order['number'] + '处理记录是什么？')[-1][1]
    check('999' not in forged['answer'] and forged['business']['warning'], 'forged explanation falls back to valid facts')
    check(len(requests()) - start == 2, 'failed explanation does not retry model')
    mode('ok')
    skill = next(s for s in api('GET', '/api/skills', admin)['items'] if s['id'] == 'safe_self_check')
    disabled = api('PUT', '/api/skills/safe_self_check', admin, {'enabled': False, 'version': skill['version']})['skill']
    start = len(requests())
    events = turn('requester', conversation('requester'), '维修有哪些安全提醒？', False)
    check(events[-1][1]['code'] == 'skill_unavailable' and len(requests()) == start, 'disabled skill cannot use basic path or gateway')
    semantic_disabled=turn('requester',conversation('requester'),'灯坏了我可以自己处理吗',False)
    check(semantic_disabled[-1][1]['code']=='skill_unavailable','semantic routing cannot bypass disabled skill')
    api('PUT', '/api/skills/safe_self_check', admin, {'enabled': True, 'version': disabled['version']})
    mode('slow')
    cid = conversation('requester')
    before = len(requests())
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        future = pool.submit(turn, 'requester', cid, '维修时有哪些安全注意事项？', False)
        for _ in range(100):
            captured = requests()[before:]
            if any(not r.get('tools') for r in captured):
                break
            time.sleep(.02)
        skill = next(s for s in api('GET', '/api/skills', admin)['items'] if s['id'] == 'safe_self_check')
        api('PUT', '/api/skills/safe_self_check', admin, {'enabled': False, 'version': skill['version']})
        result = future.result(timeout=15)
    check(result[-1][0] == 'error', 'skill revoked during generation is not published')
    mode('ok')
    start=len(requests())
    composite=turn('requester',conversation('requester'),'三号楼201灯不亮，帮我报修，也想查看安全建议')[-1][1]
    check(composite.get('draft',{}).get('status')=='active' and '暂停使用' in composite['answer'] and len(requests())>start,'report and safety request returns guidance alongside an editable draft')
    mode('chain-orders')
    chain=turn('requester',conversation('requester'),order['number']+'这单处理记录是什么？')
    check(any(x['kind']=='detail' for x in chain[-1][1]['business'].get('additional',[])),'normal query can read a dependent real detail')
    request_id=chain[0][1]['request_id']
    assert len(request_id)==32 and all(c in '0123456789abcdef' for c in request_id)
    statement="SELECT COUNT(*) FROM audit_events WHERE request_id='"+request_id+"' AND action='mcp.call' AND outcome='success'"
    result=subprocess.run(DC+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" -N -B'],input=statement,text=True,capture_output=True)
    check(result.returncode==0 and result.stdout.strip()=='2','dependent tools share request audit and two-call budget')
    combined=turn('requester',conversation('requester'),order['number']+'这单处理记录和上门安排是什么？')
    check(any(x.get('detail',{}).get('events') for x in combined[-1][1]['business'].get('additional',[])) and any(x['kind']=='visit' for x in combined[-1][1]['business'].get('additional',[])),'history and visit requirements compose from real facts')
    request_id=combined[0][1]['request_id']
    assert len(request_id)==32 and all(c in '0123456789abcdef' for c in request_id)
    statement="SELECT COUNT(*) FROM audit_events WHERE request_id='"+request_id+"' AND action='mcp.call' AND outcome='success'"
    result=subprocess.run(DC+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" -N -B'],input=statement,text=True,capture_output=True)
    check(result.returncode==0 and result.stdout.strip()=='3','three dependent reads remain within shared budget')
    current=next(a for a in api('GET','/api/agents?manage=1',admin)['items'] if a['role']=='requester')
    removed=api('PUT','/api/agents/'+str(current['id']),admin,{**current,'tool_names':[t for t in current['tool_names'] if t!='get_work_order_detail']})['agent']
    partial=turn('requester',conversation('requester'),order['number']+'这单处理记录是什么？')[-1][1]
    check(partial['business']['kind']=='orders' and not partial['business'].get('additional') and '处理记录还没有读取' in partial['answer'],'failed dependent call retains clearly marked partial facts')
    api('PUT','/api/agents/'+str(current['id']),admin,{**current,'version':removed['version']})
finally:
    mode('ok')
    current = api('GET', '/api/agents?manage=1', admin)['items']
    for old in snapshot:
        live = next(a for a in current if a['id'] == old['id'])
        api('PUT', '/api/agents/' + str(old['id']), admin, {**old, 'version': live['version']})
    current_skills = api('GET', '/api/skills', admin)['items']
    for old in original_skills:
        live = next(s for s in current_skills if s['id'] == old['id'])
        if live['enabled'] != old['enabled']:
            api('PUT', '/api/skills/' + old['id'], admin, {'enabled': old['enabled'], 'version': live['version']})
print('PASS skill execution checks ' + str(checks))
