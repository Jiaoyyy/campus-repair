"""Restore/upgrade/rollback rehearsal; writes only a dedicated recovery-check project.
Backups stay in ignored tmp/visits with mode 600. Never prints credentials or dumps.
"""
import hashlib,json,subprocess,time
from pathlib import Path
root=Path(__file__).resolve().parents[1];out=root/'tmp/visits';out.mkdir(exist_ok=True)
source=['docker','compose','-p','campus-repair-draft-check','--env-file','tmp/draft-check.env','-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml','-f','compose.draft-check.yaml']
project='campus-repair-visit-recovery-check'
if not project.endswith('-recovery-check'):raise SystemExit('Dedicated recovery fixture required')
def run(args,data=None):
 r=subprocess.run(args,cwd=root,input=data,capture_output=True)
 if r.returncode:raise RuntimeError('Recovery operation failed; raw output suppressed')
 return r.stdout
def dc(args,*tail,data=None):return run(args+list(tail),data)
client=['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" --batch --skip-column-names']
def sql(c,q):return run(c+client,q.encode()).decode().strip()
def signature(c):return hashlib.sha256(sql(c,'SELECT id,number,status,version FROM work_orders ORDER BY id').encode()).hexdigest()
def restore(c,data):
 # Only the dedicated recovery database is replaced. No operation targets source DB.
 assert c is target
 run(c+['stop','api'])
 run(c+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root -e "DROP DATABASE IF EXISTS campus_repair; CREATE DATABASE campus_repair CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"'])
 run(c+client,data)
def ready(c):
 for _ in range(100):
  try:
   if b'ready' in run(c+['exec','-T','api','wget','-q','-O','-','http://127.0.0.1:8080/health/ready']):return
  except RuntimeError:pass
  time.sleep(.2)
 raise RuntimeError('Recovery API not ready')
def manifest(volume):
 script="import pathlib,hashlib,json; p=pathlib.Path('/backup'); print(json.dumps(sorted((str(f.relative_to(p)),hashlib.sha256(f.read_bytes()).hexdigest()) for f in p.rglob('*') if f.is_file())))"
 return run(['docker','run','--rm','-v',volume+':/backup:ro','python:3.12-alpine','python','-c',script])
def image_file(image):
 p=out/'recovery-image.yaml';p.write_text('services:\n  api:\n    image: '+image+'\n');return ['-f',str(p)]
values=(root/'tmp/draft-check.env').read_text().splitlines();values=[('WEB_PORT=18087' if l.startswith('WEB_PORT=') else l) for l in values]
envp=out/'recovery.env';envp.write_text('\n'.join(values)+'\n');envp.chmod(0o600)
target=['docker','compose','-p',project,'--env-file',str(envp),'-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml']
# A consistent post-feature database + uploads snapshot, read-only volume access.
run(source+['stop','api','worker'])
try:
 post=run(source+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -u root --single-transaction --routines --triggers "$MYSQL_DATABASE"'])
 post_sig=signature(source)
 cid=run(source+['ps','-aq','api']).decode().strip();info=json.loads(run(['docker','inspect',cid]))[0]
 volume=next(m['Name'] for m in info['Mounts'] if m['Destination']=='/data/uploads')
 files=manifest(volume)
 tar=run(['docker','run','--rm','-v',volume+':/backup:ro','alpine:3.22','tar','-C','/backup','-cf','-','.'])
 for name,data in [('after.sql',post),('uploads.tar',tar)]:p=out/name;p.write_bytes(data);p.chmod(0o600)
finally:run(source+['start','api','worker'])
run(target+['up','-d','db','uploads-init'])
for _ in range(100):
 try:sql(target,'SELECT 1');break
 except RuntimeError:time.sleep(.3)
else:raise RuntimeError('Recovery DB not ready')
# Empty initialization by current image, and repeat initialization.
restore(target,b'')
run(target+['up','-d','--no-build','api']);ready(target)
assert sql(target,'SELECT COUNT(*) FROM schema_migrations WHERE version IN (7,8)')=='2'
print('PASS empty database initializes new visit schema',flush=True)
pre=(out/'campus-repair-draft-check-before.sql').read_bytes();restore(target,pre)
run(target+image_file('campus-repair-api:pre-visits-local')+['up','-d','--no-build','api']);ready(target)
pre_sig=signature(target)
assert sql(target,"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='work_order_visits'")=='0'
print('PASS restored pre-feature snapshot runs with prior image',flush=True)
run(target+image_file('campus-repair-api:latest')+['up','-d','--no-build','api']);ready(target)
assert signature(target)==pre_sig
assert sql(target,'SELECT COUNT(*) FROM schema_migrations WHERE version IN (7,8)')=='2'
run(target+['restart','api']);ready(target);assert signature(target)==pre_sig
print('PASS old database upgrade and repeated initialization preserve orders',flush=True)
restore(target,post)
run(target+['up','-d','--no-build','api']);ready(target)
assert signature(target)==post_sig
assert sql(target,'SELECT COUNT(*) FROM work_order_visits')==sql(source,'SELECT COUNT(*) FROM work_order_visits')
# Restore paired upload archive into this recovery project's own volume.
cid=run(target+['ps','-q','api']).decode().strip();info=json.loads(run(['docker','inspect',cid]))[0]
vol=next(m['Name'] for m in info['Mounts'] if m['Destination']=='/data/uploads')
run(['docker','run','--rm','-i','-v',vol+':/restore','alpine:3.22','tar','-C','/restore','-xf','-'],tar)
restored=run(['docker','run','--rm','-v',vol+':/backup:ro','alpine:3.22','tar','-C','/backup','-cf','-','.'])
assert manifest(vol)==files
print('PASS post-feature database and paired uploads restore',flush=True)
restore(target,pre)
run(target+image_file('campus-repair-api:pre-visits-local')+['up','-d','--no-build','api']);ready(target)
assert signature(target)==pre_sig
print('PASS rollback uses prior snapshot plus prior image',flush=True)
run(target+['stop','api','db'])
(out/'recovery-results.json').write_text(json.dumps({'passed':True,'project':project,'checks':5,'volumes_deleted':False},indent=2)+'\n')
