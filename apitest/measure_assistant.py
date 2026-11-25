"""Mock-only before/after timing and retained-volume rollback check.
Only the fixed tools-skills-check fixture is accepted. Outputs no credentials.
"""
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import time
import urllib.request

ROOT=Path(__file__).resolve().parents[1]
HOME=ROOT/'tmp/tools-skills-check'
CONFIG=HOME/'compose.json'
DC=['docker','compose','-p','campus-repair-tools-skills-check','--project-directory',str(ROOT),'-f',str(CONFIG)]
BASE='http://127.0.0.1:18090'


def command(args,data=None):
    result=subprocess.run(args,input=data,capture_output=True,text=True)
    if result.returncode:raise RuntimeError('Isolated measurement operation failed; raw output suppressed')
    return result.stdout.strip()


def api(path,token=None,body=None):
    headers={'Content-Type':'application/json'}
    if token:headers['Authorization']='Bearer '+token
    with urllib.request.urlopen(urllib.request.Request(BASE+path,headers=headers,data=json.dumps(body).encode() if body is not None else None),timeout=20) as response:
        return json.load(response)


def ready():
    for _ in range(60):
        try:api('/health/ready');return
        except (OSError,ValueError):time.sleep(.2)
    raise RuntimeError('Isolated API unavailable after image change')


def save_config(config):
    temporary=CONFIG.with_suffix('.measure.tmp')
    with temporary.open('w') as stream:
        os.chmod(temporary,0o600);json.dump(config,stream,indent=2)
    temporary.replace(CONFIG)


def deploy(config,image):
    modified=json.loads(json.dumps(config))
    for service in ('api','worker','mcp'):modified['services'][service]['image']=image
    save_config(modified)
    command(DC+['up','-d','--no-build','--no-deps','api','worker','mcp'])
    ready()
    target=command(['docker','image','inspect',image,'--format','{{.Id}}'])
    for service in ('api','worker','mcp'):
        actual=json.loads(command(['docker','inspect','campus-repair-tools-skills-check-'+service+'-1']))[0]
        assert actual['Image']==target,'Backend image mismatch'


def sql(statement):
    return command(DC+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" -N -B'],statement)


def snapshot():
    tables=('users','work_orders','work_order_events','repair_records','repair_drafts','work_order_visits','work_order_visit_context','skills','agent_skills','agent_tools','agent_materials','knowledge_materials')
    checks={table:sql('CHECKSUM TABLE '+table) for table in tables}
    uploads=command(DC+['exec','-T','api','sh','-c','find /data/uploads -type f -exec sha256sum {} \\; | sort'])
    checks['uploads']=hashlib.sha256(uploads.encode()).hexdigest()
    return checks


config=json.loads(CONFIG.read_text())
assert config['name']=='campus-repair-tools-skills-check'
for service in ('api','worker','mcp'):
    assert config['services'][service]['environment']['CHAT_BASE_URL']=='http://chat-mock:8080/v1'
    runtime=json.loads(command(['docker','inspect','campus-repair-tools-skills-check-'+service+'-1']))[0]
    assert runtime['Config']['Labels']['com.docker.compose.project']=='campus-repair-tools-skills-check'
    assert all(m['Type']=='volume' and m['Name'].startswith('campus-repair-tools-skills-check_') for m in runtime['Mounts'])
(HOME/'state/chat-mode').write_text('ok')
credentials=json.loads((HOME/'credentials.json').read_text())
token=api('/api/login',body={'username':'requester','password':credentials['SEED_REQUESTER_PASSWORD']})['access_token']
agent=api('/api/agents',token)['items'][0]
before=snapshot()
records=[]
try:
    for label,image in (('before','campus-repair-api:latest'),('after','campus-repair-api:tools-skills-check')):
        deploy(config,image)
        for question in ('我现在有多少工单要处理','你好','报修需要填写哪些信息？'):
            for sample in range(3):
                cid=api('/api/conversations',token,{'agent_id':agent['id']})['conversation']['id']
                request_file=HOME/'state/chat-requests.jsonl'
                prior=len(request_file.read_text().splitlines())
                started=time.perf_counter()
                request=urllib.request.Request(BASE+f'/api/conversations/{cid}/messages',headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'},data=json.dumps({'content':question}).encode())
                with urllib.request.urlopen(request,timeout=20) as response:raw=response.read().decode()
                duration=(time.perf_counter()-started)*1000
                assert 'event: done' in raw and 'event: error' not in raw,'Measurement turn failed'
                events=[]
                for block in raw.split('\n\n'):
                    data=[line[5:].strip() for line in block.splitlines() if line.startswith('data:')]
                    if data:events.append(json.loads(''.join(data)))
                request_id=events[0]['request_id']
                assert len(request_id)==32 and all(c in '0123456789abcdef' for c in request_id)
                rows=sql("SELECT metadata FROM audit_events WHERE request_id='"+request_id+"' AND action='assistant.performance'")
                stages=json.loads(rows) if rows else {}
                calls=len(request_file.read_text().splitlines())-prior
                records.append({'version':label,'scenario':question,'sample':sample+1,'duration_ms':round(duration,2),'model_calls':calls,'stages':stages})
        print('PASS '+label+' measurement samples',flush=True)
    assert snapshot()==before,'Business/settings/upload snapshots changed during rollback'
    print('PASS rollback and upgrade retained business, skill bindings and uploads',flush=True)
finally:
    save_config(config)
    deploy(config,'campus-repair-api:tools-skills-check')

summaries=[]
for label in ('before','after'):
    samples=[r for r in records if r['version']==label]
    durations=sorted(r['duration_ms'] for r in samples)
    summaries.append({'version':label,'samples':len(samples),'p50_ms':durations[math.ceil(len(durations)*.5)-1],'p95_ms':durations[math.ceil(len(durations)*.95)-1],'model_calls':sum(r['model_calls'] for r in samples)})
result={'dependency':'mock','note':'每场景3个样本，p95仅描述本次小样本；模型耗时不代表真实网关。','rollback_preserved':True,'summaries':summaries,'records':records}
(HOME/'measurement.json').write_text(json.dumps(result,ensure_ascii=False,indent=2))
print(json.dumps(summaries))
