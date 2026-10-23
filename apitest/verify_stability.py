"""Mock-only regression runner. Restores assistant settings/modes even on failure.
No real gateway calls; no configuration or credentials are printed.
"""
import argparse, json, subprocess, sys, time, urllib.request
from pathlib import Path

root=Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--project',default='campus-repair-draft-check')
p.add_argument('--env-file',default='tmp/draft-check.env')
p.add_argument('--state-dir',default='tmp/draft-mock')
a=p.parse_args()
if not a.project.startswith('campus-repair-') or not a.project.endswith('-check'):raise SystemExit('Requires isolated campus-repair-*-check project')
env_path=(root/a.env_file).resolve();state=(root/a.state_dir).resolve()
env=dict(l.split('=',1) for l in env_path.read_text().splitlines() if '=' in l and not l.startswith('#'))
base='http://127.0.0.1:'+env['WEB_PORT']
compose=['docker','compose','-p',a.project,'--env-file',str(env_path),'-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml','-f','compose.draft-check.yaml']
def dc(*args):
    r=subprocess.run(compose+list(args),cwd=root,capture_output=True,text=True)
    if r.returncode:raise RuntimeError('Isolated container operation failed; raw output suppressed')
    return r.stdout.strip()
def req(method,path,body=None,token=None):
    headers={'Content-Type':'application/json'}
    if token:headers['Authorization']='Bearer '+token
    with urllib.request.urlopen(urllib.request.Request(base+path,method=method,headers=headers,data=json.dumps(body).encode() if body is not None else None),timeout=15) as r:return json.load(r)
def ready():
    for _ in range(100):
        try:
            req('GET','/health/ready');return
        except (OSError,ValueError):time.sleep(.2)
    raise RuntimeError('Isolated API not ready')
def login():return req('POST','/api/login',{'username':'admin','password':env['SEED_ADMIN_PASSWORD']})['access_token']
# Inspect running containers, not just a config file that may differ from runtime.
images=[]
for service in ('api','worker','mcp'):
    cid=dc('ps','-q',service)
    if not cid:raise SystemExit('Isolated service missing: '+service)
    container=json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
    images.append(container['Image'])
    if service in ('api','worker'):
        values=dict(x.split('=',1) for x in container['Config']['Env'])
        if values.get('CHAT_BASE_URL')!='http://chat-mock:8080/v1' or values.get('EMBEDDING_BASE_URL')!='http://embedding-mock:8080/v1':raise SystemExit('Refusing to run: actual dependencies are not isolated mocks')
if len(set(images))!=1:raise SystemExit('Isolated backend images differ')
ready();admin=login();snapshot=req('GET','/api/agents?manage=1',token=admin)['items']
original_modes={name:(state/name).read_bytes() if (state/name).exists() else None for name in ('mode','chat-mode')}
logs=root/'tmp/stability';logs.mkdir(exist_ok=True)
suites=[
 ('assistant',[sys.executable,'apitest/verify_assistant.py','--project',a.project,'--env-file',str(env_path),'--state-dir',str(state),'--compose-file','compose.draft-check.yaml']),
 ('drafts',[sys.executable,'apitest/verify_drafts.py','--project',a.project,'--env-file',str(env_path)]),
 ('scenarios',[sys.executable,'apitest/verify_scenarios.py','--project',a.project,'--env-file',str(env_path)]),
 ('semantic',[sys.executable,'apitest/verify_semantic_failures.py']),
 ('mcp',['docker','run','--rm','--network',a.project+'_default','-v',str(root/'apitest')+':/verify/apitest:ro','-v',str(env_path)+':/verify/.env:ro','-e','API_TEST_URL=http://api:8080','-e','MCP_TEST_URL=http://mcp:8081/mcp','python:3.12-alpine','python','/verify/apitest/verify_mcp.py']),
 ('manual',[sys.executable,'apitest/verify_manual.py','--project',a.project,'--env-file',str(env_path)]),
]
# The failure probe is deliberately fixed to this named disposable environment.
if a.project!='campus-repair-draft-check' or env_path!=root/'tmp/draft-check.env':raise SystemExit('Failure probe requires the named draft-check fixture')
results=[]
try:
    for name,command in suites:
        with (logs/(name+'.log')).open('w') as out:r=subprocess.run(command,cwd=root,stdout=out,stderr=subprocess.STDOUT)
        results.append({'suite':name,'passed':r.returncode==0})
        print(('PASS ' if not r.returncode else 'FAIL ')+name,flush=True)
        if r.returncode:raise RuntimeError('Regression suite failed: '+name+'; inspect tmp/stability/'+name+'.log')
finally:
    # Manual API tests intentionally hit login limits; restart ONLY this isolated API.
    dc('restart','api');ready();admin=login()
    current=req('GET','/api/agents?manage=1',token=admin)['items']
    fields=('name','role','prompt','enabled','material_ids','tool_names')
    for old in snapshot:
        live=next(x for x in current if x['id']==old['id'])
        req('PUT',f"/api/agents/{old['id']}",{**{k:old[k] for k in fields},'version':live['version']},admin)
    restored=req('GET','/api/agents?manage=1',token=admin)['items']
    assert all(all(next(x for x in restored if x['id']==old['id'])[k]==old[k] for k in fields) for old in snapshot),'Fixture restoration mismatch'
    for name,value in original_modes.items():
        path=state/name
        if value is None:path.unlink(missing_ok=True)
        else:path.write_bytes(value)
    (logs/'results.json').write_text(json.dumps({'gateway':'mock_only','project':a.project,'results':results,'settings_restored':True},indent=2)+'\n')
    print('PASS assistant settings and fixture modes restored',flush=True)
