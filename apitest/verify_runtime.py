"""Run dependency, restart and secret checks only against an isolated test project."""
import subprocess,urllib.request,urllib.error,json,time,sys
from pathlib import Path
root=Path(__file__).resolve().parents[1]
project='campus-repair-check';envfile='tmp/check.env'
env=dict(l.split('=',1) for l in (root/envfile).read_text().splitlines() if '=' in l and not l.startswith('#'))
base='http://127.0.0.1:'+env['WEB_PORT']
compose=['docker','compose','-p',project,'--env-file',envfile,'-f','compose.yaml','-f','compose.verify.yaml']
def run(cmd,input=None):
 p=subprocess.run(cmd,cwd=root,input=input,text=True,capture_output=True)
 if p.returncode:raise RuntimeError('Isolated runtime operation failed')
 return p.stdout
def sql(query):return run(compose+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" --batch --skip-column-names'],query)
def status(path):
 try:
  with urllib.request.urlopen(base+path,timeout=5) as r:return r.status
 except urllib.error.HTTPError as e:return e.code
 except Exception:return 0
def ready():
 for i in range(60):
  if status('/health/ready')==200:return
  time.sleep(.5)
 raise RuntimeError('Readiness did not recover')
ready()
run(compose+['stop','db']);assert status('/health/live')==200 and status('/health/ready')==503,'health distinction'
run(compose+['start','db']);ready();print('PASS: database outage gives ready 503, live 200; readiness recovers.')
query="SELECT id,username,role,enabled FROM users ORDER BY id; SELECT id,number,status,version FROM work_orders ORDER BY id; SELECT COUNT(*) FROM work_order_events; SELECT COUNT(*) FROM repair_records; SELECT COUNT(*) FROM audit_events;"
before=sql(query)
run(compose+['down']);run(compose+['up','-d','--no-build']);ready()
after=sql(query);assert before==after,'persistent data changed on restart';print('PASS: restart keeps accounts, work orders, versions, events, repairs and audit; seeds not duplicated.')
logs=run(compose+['logs','--no-color','api','web'])
frontend='\n'.join(p.read_text(errors='replace') for p in (root/'frontend/dist').rglob('*') if p.is_file())
secrets=[v for k,v in env.items() if ('PASSWORD' in k or 'SECRET' in k) and v]
assert all(v not in logs and v not in frontend for v in secrets),'secret leaked'
print('PASS: configured secrets absent from API/web logs and frontend assets.')
