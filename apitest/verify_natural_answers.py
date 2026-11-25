"""Replay fictional answer-quality cases against the dedicated mock fixture."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = ROOT / 'tmp/tools-skills-check'
OUTPUT = ROOT / 'tmp/natural-answers'
PROJECT = 'campus-repair-tools-skills-check'
BASE = 'http://127.0.0.1:18090'
DC = ['docker', 'compose', '-p', PROJECT, '--project-directory', str(ROOT), '-f', str(FIXTURE / 'compose.json')]
ROLES = {'admin': 'admin', 'requester': 'requester', 'tech': 'technician'}

def request(path, token=None, body=None, method=None):
    headers = {'Content-Type': 'application/json', 'Idempotency-Key': str(uuid.uuid4())}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    data = None if body is None else json.dumps(body).encode()
    with urllib.request.urlopen(urllib.request.Request(BASE+path, data=data, headers=headers, method=method), timeout=20) as response:
        return json.load(response)

def sql(statement):
    result = subprocess.run(DC + ['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -N -B'], input=statement, text=True, capture_output=True)
    if result.returncode != 0:
        raise RuntimeError('Isolated database query failed')
    return result.stdout.strip()

def stream(token, cid, question):
    req = urllib.request.Request(BASE+f'/api/conversations/{cid}/messages', data=json.dumps({'content':question}).encode(),
                                 headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
    events = []
    with urllib.request.urlopen(req, timeout=30) as response:
        kind = ''
        for line in response:
            if line.startswith(b'event:'):
                kind = line[6:].decode().strip()
            elif line.startswith(b'data:'):
                events.append((kind,json.loads(line[5:])))
    if not events or events[0][0] != 'meta' or events[-1][0] not in ('done','error'):
        raise AssertionError('broken SSE response')
    return events

def fixture():
    runtime = json.loads(subprocess.check_output(['docker','inspect',PROJECT+'-api-1'],text=True))[0]
    assert runtime['Config']['Labels']['com.docker.compose.project'] == PROJECT
    assert 'CHAT_BASE_URL=http://chat-mock:8080/v1' in runtime['Config']['Env']
    assert all(m['Type']=='volume' and m['Name'].startswith(PROJECT+'_') for m in runtime['Mounts'])
    cfg = json.loads((FIXTURE/'credentials.json').read_text())
    return {name:request('/api/login',body={'username':name,'password':cfg['SEED_'+name.upper()+'_PASSWORD']})['access_token'] for name in ROLES}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('stage',choices=['before','after'])
    args = parser.parse_args()
    OUTPUT.mkdir(exist_ok=True)
    tokens = fixture()
    admin = tokens['admin']
    snapshots = request('/api/agents?manage=1',admin)['items']
    tools = request('/api/mcp/tools',admin)['items']
    material = request('/api/materials?page_size=100',admin)['items']
    mode = FIXTURE/'state/chat-mode'
    old_mode = mode.read_text() if mode.exists() else None
    results = []
    try:
        agents = {}
        for old in snapshots:
            updates = dict(old, enabled=True, tool_names=[t['name'] for t in tools if t['role'] in ('all',old['role'])],
                           material_ids=[m['id'] for m in material if m['status']=='ready'])
            agents[old['role']] = request(f"/api/agents/{old['id']}",admin,updates,'PUT')['agent']['id']
        state_path = OUTPUT/'scenario.json'
        if args.stage == 'before':
            location = '分析验收楼'+uuid.uuid4().hex[:8]
            orders = [request('/api/work-orders',tokens['requester'],{'location':location,'category':'lighting','description':'虚构走廊灯不亮'})['order'] for _ in range(7)]
            ids = ','.join(str(int(row['id'])) for row in orders)
            sql(f'UPDATE work_orders SET created_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 14 DAY) WHERE id IN ({ids})')
            state_path.write_text(json.dumps({'location':location,'number':orders[0]['number']},ensure_ascii=False))
        state = json.loads(state_path.read_text())
        conversations = {}
        for case in json.loads((ROOT/'apitest/natural_answer_cases.json').read_text()):
            name = case['role']
            token = tokens[name]
            cid = conversations.get(case.get('after'))
            if not cid:
                cid = request('/api/conversations',token,{'agent_id':agents[ROLES[name]]})['conversation']['id']
            conversations[case['id']] = cid
            question = case['question'].format(**state)
            mode.write_text('insight' if case['id'] in ('current','which','why_first') else 'safe-warning' if case['id']=='safety' else 'natural-quality' if case['id'] in ('recent','compare') else 'ok')
            start = time.perf_counter()
            events = stream(token,cid,question)
            terminal, data = events[-1]
            request_id = events[0][1]['request_id']
            assert len(request_id)==32 and all(c in '0123456789abcdef' for c in request_id)
            query = f"SELECT action,metadata FROM audit_events WHERE request_id='{request_id}' AND action IN ('assistant.performance','mcp.call') ORDER BY id"
            trace = [row.split('\t',1) for row in sql(query).splitlines()]
            business = data.get('business') or {}
            results.append({**case,'question':question,'request_id':request_id,'duration_ms':round((time.perf_counter()-start)*1000),
                            'terminal':terminal,'answer':data.get('answer'),'error':data.get('code'),'warning':business.get('warning'),
                            'business':business,'citations':data.get('citations',[]),'draft_status':(data.get('draft') or {}).get('status'),
                            'trace':[{'action':row[0],'metadata':json.loads(row[1])} for row in trace]})
            print(case['id'],terminal,'natural='+str(bool(business.get('natural'))),flush=True)
    finally:
        if old_mode is None:
            mode.unlink(missing_ok=True)
        else:
            mode.write_text(old_mode)
        current = {a['id']:a for a in request('/api/agents?manage=1',admin)['items']}
        for old in snapshots:
            request(f"/api/agents/{old['id']}",admin,dict(old,version=current[old['id']]['version']),'PUT')
        path = OUTPUT/(args.stage+'.json')
        path.write_text(json.dumps(results,ensure_ascii=False,indent=2))
        os.chmod(path,0o600)

if __name__ == '__main__':
    main()
