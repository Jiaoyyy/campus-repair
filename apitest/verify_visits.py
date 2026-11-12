"""Visit contracts in the named isolated mock fixture only. Never uses a real gateway."""
import concurrent.futures, datetime as dt, json, subprocess, time, urllib.error, urllib.request, uuid
from pathlib import Path
root=Path(__file__).resolve().parents[1]
project='campus-repair-draft-check'
env=dict(l.split('=',1) for l in (root/'tmp/draft-check.env').read_text().splitlines() if '=' in l and not l.startswith('#'))
base='http://127.0.0.1:'+env['WEB_PORT']
compose=['docker','compose','-p',project,'--env-file','tmp/draft-check.env','-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml','-f','compose.draft-check.yaml']
def dc(*args,input=None):
 r=subprocess.run(compose+list(args),input=input,cwd=root,capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Isolated container operation failed; raw output suppressed')
 return r.stdout.strip()
def sql(q):return dc('exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" --batch --skip-column-names',input=q)
def check(ok,label):
 if not ok:raise AssertionError(label)
 checks.append(label);print('PASS '+label,flush=True)
def req(method,path,token=None,body=None,key=None):
 h={'Content-Type':'application/json'}
 if token:h['Authorization']='Bearer '+token
 if key:h['Idempotency-Key']=key
 try:
  with urllib.request.urlopen(urllib.request.Request(base+path,method=method,headers=h,data=json.dumps(body).encode() if body is not None else None),timeout=15) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as r:return r.code,json.load(r)
def expect(code,result,label):check(code==result[0],label+' HTTP '+str(result[0]));return result[1]
def login(name,key):return expect(200,req('POST','/api/login',body={'username':name,'password':env[key]}),'login '+name)['access_token']
def order(token=None):return expect(201,req('POST','/api/work-orders',token or user,{'category':'lighting','location':'演示三号楼201','description':'演示灯具故障'},str(uuid.uuid4())),'create')['order']
def detail(o,token=None):return expect(200,req('GET',f"/api/work-orders/{o['id']}",token or user),'detail')['order']
def act(o,name,token=None,**data):
 return req('POST',f"/api/work-orders/{o['id']}/{name}",token or tech,{'version':o['version'],**data})
def visit(o,token=None):return expect(200,req('GET',f"/api/work-orders/{o['id']}/visit",token or user),'visit')
def write(o,name,token=None,**data):return req('PUT' if name=='availability' else 'POST',f"/api/work-orders/{o['id']}/visit/"+('availability' if name=='availability' else 'actions/'+name),token or (user if name=='availability' else tech),{'version':o['version'],**data})
def do(o,name,token=None,**data):expect(200,write(o,name,token,**data),name);return detail(o)
def assigned(o):
 o=expect(200,act(o,'review',admin,decision='approve'),'approve')['order']
 o=expect(200,act(o,'assign',admin,technician_id=tid),'assign')['order'];return o
TZ=dt.timezone(dt.timedelta(hours=8))
def window(hours=1):
 start=(dt.datetime.now(TZ)+dt.timedelta(hours=hours)).replace(microsecond=0)
 return {'start':start.isoformat(),'end':(start+dt.timedelta(hours=2)).isoformat()}
checks=[]
# Refuse fixtures whose actual API is attached to a real model.
cid=dc('ps','-q','api');info=json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
values=dict(x.split('=',1) for x in info['Config']['Env'])
if values.get('CHAT_BASE_URL')!='http://chat-mock:8080/v1':raise SystemExit('Mock fixture required')
for _ in range(100):
 try:
  if req('GET','/health/ready')[0]==200:break
 except (OSError,ValueError):pass
 time.sleep(.2)
admin=login('admin','SEED_ADMIN_PASSWORD');user=login('requester','SEED_REQUESTER_PASSWORD');second=login('requester2','SEED_REQUESTER2_PASSWORD');other=login('other','SEED_OTHER_PASSWORD');other_admin=login('other_admin','SEED_OTHER_PASSWORD');tech=login('tech','SEED_TECH_PASSWORD');tech2=login('tech2','SEED_TECH2_PASSWORD')
tid=int(sql("SELECT id FROM users WHERE username='tech'"));tid2=int(sql("SELECT id FROM users WHERE username='tech2'"))
check(sql('SELECT COUNT(*) FROM schema_migrations WHERE version=7')=='1','migration7 applied once')
o=order();empty=visit(o);check(empty['context']['visit'] is None,'legacy order has no appointment')
for token in (second,other,tech,tech2):
 expect(404,req('GET',f"/api/work-orders/{o['id']}/visit",token),'invisible visit')
 expect(404,write(o,'availability',token,**window()),'invisible write')
expect(401,req('GET',f"/api/work-orders/{o['id']}/visit"),'anonymous')
expect(404,req('POST',f"/api/work-orders/{o['id']}/visit/actions/availability",user,{'version':o['version']}),'availability is PUT only')
expect(403,write(o,'availability',admin,**window()),'admin cannot set availability')
expect(400,write(o,'availability',**{**window(),'org_id':2}),'identity field rejected')
expect(400,write(o,'availability',start='2026-10-23T10:00:00',end='bad'),'zone required')
expect(400,write(o,'availability',start=window()['start']),'half window')
expect(400,write(o,'availability',**window(-10)),'past window')
expect(400,write(o,'availability',**window(15*24)),'horizon rejected')
f=window();o=do(o,'availability',**f)
expect(409,write({**o,'version':o['version']-1},'availability',**f),'stale version')
o=assigned(o)
expect(409,write(o,'schedule',tech,**f),'must accept first')
o=expect(200,act(o,'accept'),'accept')['order']
expect(403,write(o,'schedule',admin,**f),'admin cannot schedule')
expect(400,write(o,'schedule',tech,**window(5)),'outside availability')
o=do(o,'schedule',**f)
expect(400,write(o,'availability',**window(3)),'change confirmed requires reason')
expect(409,write(o,'arrive'),'arrival before departure')
before=o.copy();o=do(o,'depart')
expect(409,write(before,'depart'),'lost response replay rejected')
check(visit(o)['context']['visit']['stage']=='departed','lost response resolved by reread')
expect(409,write(o,'availability',**window(3),reason='改时间'),'no change while travelling')
o=do(o,'arrive');o=expect(200,act(o,'complete',measures='更换灯具',result='演示测试修复'),'complete')['order'];o=expect(200,act(o,'confirm',user),'requester accepts')['order']
check(visit(o)['context']['visit'] is None,'complete invalidates appointment')
expect(409,write(o,'availability',**window()),'terminal immutable')
# Absence requires a new preference and new arrival, and counts exactly one per visit.
o=assigned(order());o=expect(200,act(o,'accept'),'accept')['order'];f=window();o=do(o,'availability',**f);o=do(o,'schedule',**f);o=do(o,'depart');o=do(o,'arrive')
expect(400,write(o,'no-show',reason=' '),'absence needs reason')
o=do(o,'no-show',reason='演示：敲门后无人应答');old=o.copy()
expect(409,write(o,'no-show',reason='重复'),'same appointment absence cannot repeat')
expect(409,write(o,'schedule',**window()),'new availability required')
expect(409,act(o,'complete',measures='无',result='不能结单'),'absence blocks completion')
check(sql(f"SELECT COUNT(*) FROM work_order_visits WHERE work_order_id={o['id']} AND no_show_at IS NOT NULL")=='1','one absence event')
o=do(o,'wait-parts',reason='演示：无人且备件不足',expected_resume_at=window()['start']);o=do(o,'resume',reason='备件到货')
check(visit(o)['context']['exception']=='no_show','parts recovery preserves unresolved absence')
expect(409,act(o,'complete',measures='无',result='不能绕过重新约时'),'parts cannot bypass revisit')
f=window(3);o=do(o,'availability',**f);o=do(o,'schedule',**f)
expect(409,act(o,'complete',measures='无',result='尚未到场'),'rescheduled but not arrived still blocked')
o=do(o,'depart');o=do(o,'arrive');o=do(o,'no-show',reason='演示：第二次预约无人')
check(sql(f"SELECT COUNT(*) FROM work_order_visits WHERE work_order_id={o['id']} AND no_show_at IS NOT NULL")=='2','two appointments counted separately')
absence_id=o['id']
f=window(5);o=do(o,'availability',**f);o=do(o,'schedule',**f);o=do(o,'depart');o=do(o,'arrive');o=expect(200,act(o,'complete',measures='修复线路',result='已修复'),'complete after new arrival')['order'];o=expect(200,act(o,'confirm',user),'confirm')['order']
# Parts and stale ETA do not automatically recover.
o=assigned(order());o=expect(200,act(o,'accept'),'accept')['order']
expect(400,write(o,'wait-parts',reason='备件缺货'),'ETA required')
o=do(o,'wait-parts',reason='演示：等待灯管',expected_resume_at=window()['start'])
expect(409,act(o,'complete',measures='无',result='不能结单'),'parts blocks completion')
sql(f"UPDATE work_order_visit_context SET expected_resume_at=UTC_TIMESTAMP(6)-INTERVAL 1 HOUR WHERE work_order_id={o['id']}")
check(visit(o)['context']['exception']=='waiting_parts','expired ETA does not auto resume')
expect(400,write(o,'resume',reason=''),'resume needs explanation')
o=do(o,'resume',reason='演示：备件到货');o=expect(200,act(o,'complete',measures='更换',result='修复'),'parts recovery complete')['order'];o=expect(200,act(o,'confirm',user),'confirm')['order']
# Reassignment revokes old technician; legacy state paths still work.
o=assigned(order());o=do(o,'availability',**window())
o=expect(200,act(o,'reassign',admin,technician_id=tid2,reason='演示调度'),'reassign')['order']
expect(404,req('GET',f"/api/work-orders/{o['id']}/visit",tech),'old assignee read revoked')
expect(404,write(o,'schedule',tech,**window()),'old assignee write revoked')
o=expect(200,act(o,'accept',tech2),'new technician accepts')['order'];o=expect(200,act(o,'complete',tech2,measures='无需预约直接处理',result='演示完成'),'legacy no appointment complete')['order'];o=expect(200,act(o,'reopen',user,reason='仍有问题'),'reopen')['order'];check(visit(o)['context']['visit'] is None,'reopen no automatic appointment')
# Race: only one actor can update this version.
o=assigned(order());o=expect(200,act(o,'accept'),'accept')['order'];f=window();o=do(o,'availability',**f);o=do(o,'schedule',**f)
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
 futures=[pool.submit(write,o,'depart'),pool.submit(write,o,'availability',None,**window(3),reason='演示调整')];codes=sorted(x.result()[0] for x in futures)
check(codes==[200,409],'availability versus depart atomic')
# Schedule versus completion and exception versus resume races.
x=assigned(order());x=expect(200,act(x,'accept'),'accept')['order'];f=window();x=do(x,'availability',**f)
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
 codes=sorted(v.result()[0] for v in [pool.submit(write,x,'schedule',None,**f),pool.submit(act,x,'complete',None,measures='演示',result='完成')])
check(codes==[200,409],'schedule versus completion atomic')
x=assigned(order());x=expect(200,act(x,'accept'),'accept')['order'];x=do(x,'wait-parts',reason='演示',expected_resume_at=window()['start'])
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
 codes=sorted(v.result()[0] for v in [pool.submit(write,x,'resume',None,reason='已到货'),pool.submit(write,x,'wait-parts',None,reason='更新',expected_resume_at=window()['start'])])
check(codes==[200,409],'exception versus resume atomic')
# Transaction rollback from audit fault injection in this isolated database only.
x=order();before=visit(x)
try:
 sql("CREATE TRIGGER visit_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated audit failure'")
 expect(503,write(x,'availability',**window()),'audit failure')
finally:sql('DROP TRIGGER IF EXISTS visit_audit_failure')
after=visit(x);check(before['version']==after['version'] and before['context']==after['context'],'context and version rolled back')
# Queue statistics and expiry are based on actual valid appointments.
x=assigned(order());x=expect(200,act(x,'accept'),'accept')['order'];f=window();x=do(x,'availability',**f);x=do(x,'schedule',**f)
sql(f"UPDATE work_order_visits SET starts_at=UTC_TIMESTAMP(6)-INTERVAL 2 HOUR,ends_at=UTC_TIMESTAMP(6)-INTERVAL 1 HOUR WHERE work_order_id={x['id']}")
expect(409,write(x,'depart'),'expired departure refused')
board=expect(200,req('GET','/api/repair-workbench?view=overdue',admin),'overdue board')
check(any(v['order']['id']==x['id'] for v in board['items']),'expired appointment appears')
met=expect(200,req('GET','/api/repair-workbench/metrics',admin),'metrics');check(met['counts']['overdue']==board['total'],'overdue metric matches queue')
check(met['counts']['no_show']==int(sql('SELECT COUNT(*) FROM work_order_visits v JOIN work_orders o ON o.id=v.work_order_id WHERE o.org_id=1 AND v.no_show_at>=UTC_DATE()-INTERVAL 6 DAY')),'absence metric matches facts')
check(expect(200,req('GET','/api/repair-workbench/metrics',other_admin),'isolated org metrics')['counts']['no_show']==0,'cross org metrics isolated')
for token in (user,tech):expect(403,req('GET','/api/repair-workbench/metrics',token),'metrics restricted')
expect(403,req('GET','/api/repair-workbench',user),'requester cannot query workbench')
expect(400,req('GET','/api/repair-workbench?view=overdue',tech),'role queue rejected')
expect(400,req('GET','/api/repair-workbench/metrics?date_from=2024-01-01&date_to=2025-01-01',admin),'range too long')
for v in ['pending','unscheduled','today','processing']:
 b=expect(200,req('GET','/api/repair-workbench?view='+v+'&page_size=1',tech),'technician queue '+v)
 check(b['total']==b['counts'][v] and len(b['items'])<=1,'full count independent of page')
 check(all(i['order']['assignee_id']==tid and i['order']['org_id']==1 for i in b['items']),'technician scope')
# Compare today's queue across Beijing boundaries with the database's ordered facts.
b=expect(200,req('GET','/api/repair-workbench?view=today&page_size=100',tech),'today ordering')
start=dt.datetime.now(TZ).replace(hour=0,minute=0,second=0,microsecond=0).astimezone(dt.timezone.utc);end=start+dt.timedelta(days=1)
expected=sql(f"SELECT o.id FROM work_orders o JOIN work_order_visit_context c ON c.work_order_id=o.id JOIN work_order_visits v ON v.id=c.active_visit_id WHERE o.org_id=1 AND o.assignee_id={tid} AND v.starts_at>='{start:%Y-%m-%d %H:%M:%S}' AND v.starts_at<'{end:%Y-%m-%d %H:%M:%S}' ORDER BY v.starts_at,o.id LIMIT 100")
check([i['order']['id'] for i in b['items']]==[int(n) for n in expected.splitlines() if n],'today order matches complete authorized facts')
# A deterministic midnight fixture; restore actual dates after the check.
dates=sql(f"SELECT id,DATE_FORMAT(no_show_at,'%Y-%m-%d %H:%i:%s.%f') FROM work_order_visits WHERE work_order_id={absence_id} AND no_show_at IS NOT NULL ORDER BY id").splitlines()
try:
 for row,when in zip(dates,['2020-01-01 15:59:59','2020-01-01 16:00:00']):sql("UPDATE work_order_visits SET no_show_at='"+when+"' WHERE id="+row.split('\t')[0])
 for day in ['2020-01-01','2020-01-02']:
  m=expect(200,req('GET','/api/repair-workbench/metrics?date_from='+day+'&date_to='+day,admin),'Beijing midnight '+day)
  check(m['counts']['no_show']==1,'one visit on each side of midnight')
finally:
 for row in dates:
  vid,when=row.split('\t');sql("UPDATE work_order_visits SET no_show_at='"+when+"' WHERE id="+vid)
# Disabled/revoked session checked by the real authentication middleware.
try:
 sql("UPDATE users SET enabled=FALSE WHERE username='tech'")
 expect(401,req('GET',f"/api/work-orders/{x['id']}/visit",tech),'disabled account read')
finally:sql("UPDATE users SET enabled=TRUE WHERE username='tech'")
t=login('tech','SEED_TECH_PASSWORD');expect(200,req('POST','/api/logout',t),'logout')
expect(401,write(x,'depart',t),'revoked session write')
# Repeated initialization and persistence.
before=visit(x);old_count=sql('SELECT COUNT(*) FROM work_orders');dc('restart','api')
for _ in range(100):
 try:
  if req('GET','/health/ready')[0]==200:break
 except (OSError,ValueError):pass
 time.sleep(.2)
after=visit(x);check(before['context']==after['context'] and before['version']==after['version'],'visit persists across restart')
check(sql('SELECT COUNT(*) FROM work_orders')==old_count and sql('SELECT COUNT(*) FROM schema_migrations WHERE version=7')=='1','repeat initialization leaves orders unchanged')
(root/'tmp/visits/results.json').write_text(json.dumps({'project':project,'gateway':'not_called','checks':len(checks),'passed':True},indent=2)+'\n')
print('VISITS PASSED',len(checks))
