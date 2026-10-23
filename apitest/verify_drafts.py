"""Conversational repair contract; destructive fault checks ONLY in a separate *-check project."""
import argparse, concurrent.futures, json, subprocess, time, urllib.error, urllib.request
from pathlib import Path

p=argparse.ArgumentParser()
p.add_argument('--project',default='campus-repair-draft-check')
p.add_argument('--env-file',default='tmp/draft-check.env')
a=p.parse_args()
if not a.project.endswith('-check'):raise SystemExit('Requires isolated *-check project')
root=Path(__file__).resolve().parents[1]
env=dict(l.split('=',1) for l in (root/a.env_file).read_text().splitlines() if '=' in l and not l.startswith('#'))
base='http://127.0.0.1:'+env['WEB_PORT'];checks=[]
compose=['docker','compose','-p',a.project,'--env-file',a.env_file,'-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml','-f','compose.draft-check.yaml']
def dc(*argv,input=None):
 r=subprocess.run(compose+list(argv),input=input,cwd=root,capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Isolated operation failed; raw output suppressed')
 return r.stdout.strip()
def sql(q):return dc('exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" --batch --skip-column-names',input=q)
def mode(v):dc('exec','-T','chat-mock','python','-c',"from pathlib import Path; Path('/state/chat-mode').write_text("+repr(v)+")")
def check(ok,label):
 if not ok:raise AssertionError(label)
 checks.append(label);print('PASS '+label,flush=True)
def req(method,path,token=None,body=None,key=None):
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 if key:headers['Idempotency-Key']=key
 data=json.dumps(body).encode() if body is not None else None
 try:
  with urllib.request.urlopen(urllib.request.Request(base+path,method=method,headers=headers,data=data),timeout=15) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as r:return r.code,json.load(r)
def expect(code,res,label):check(res[0]==code,label+' HTTP '+str(res[0]));return res[1]
def login(name,key):return expect(200,req('POST','/api/login',body={'username':name,'password':env[key]}),'login '+name)['access_token']
def create():return expect(201,req('POST','/api/conversations',user,{'agent_id':aid}),'new conversation')['conversation']['id']
def history(cid):return expect(200,req('GET',f'/api/conversations/{cid}/messages',user),'history')['items']
def stream(cid,text):
 return urllib.request.urlopen(urllib.request.Request(base+f'/api/conversations/{cid}/messages',headers={'Authorization':'Bearer '+user,'Content-Type':'application/json'},data=json.dumps({'content':text}).encode()),timeout=15)
def event(res):
 kind='';data=[]
 for raw in res:
  line=raw.decode().strip()
  if line.startswith('event:'):kind=line[6:].strip()
  elif line.startswith('data:'):data.append(line[5:].strip())
  elif not line and kind:return kind,json.loads(''.join(data))
def turn(cid,text,success=True):
 events=[]
 with stream(cid,text) as res:
  while True:
   e=event(res)
   if e is None:break
   events.append(e)
 check(events[0][0]=='meta' and sum(k in ('done','error') for k,_ in events)==1,'one terminal event')
 check(events[-1][0]==('done' if success else 'error'),'turn '+('complete' if success else 'failed'))
 return events[-1][1]
def path(d):return f"/api/conversations/{d['conversation_id']}/drafts/{d['message_id']}"
def fields(d):return {k:d[k] for k in ('category','location','description')}
def save(d,token=None,**changes):return req('PUT',path(d),token or user,{**fields(d),'version':d['version'],'status':'active',**changes})
def submit(d,token=None,**changes):return req('POST','/api/work-orders',token or user,{**fields(d),'draft_message_id':d['message_id'],'draft_version':d['version'],**changes},d['idempotency_key'])
def counts():return sql("SELECT CONCAT((SELECT COUNT(*) FROM work_orders),':',(SELECT COUNT(*) FROM work_order_events),':',(SELECT COUNT(*) FROM audit_events WHERE action='work_order.create'))")
def complete():
 cid=create();d=turn(cid,'三号楼201宿舍灯坏了，帮我报修')['draft'];check(d['location']=='三号楼201' and d['description']=='宿舍灯坏了','complete structured draft');return d
mode('ok')
for _ in range(100):
 try:
  if req('GET','/health/ready')[0]==200:break
 except Exception:pass
 time.sleep(.2)
else:raise RuntimeError('API unavailable')
admin=login('admin','SEED_ADMIN_PASSWORD');user=login('requester','SEED_REQUESTER_PASSWORD');second=login('requester2','SEED_REQUESTER2_PASSWORD');other=login('other','SEED_OTHER_PASSWORD');tech=login('tech','SEED_TECH_PASSWORD')
agents=expect(200,req('GET','/api/agents',user),'agents')['items'];aid=agents[0]['id']
cid=create();before=counts();d=turn(cid,'宿舍灯坏了，帮我报修')['draft']
check(d['category']=='lighting' and not d['location'] and d['description']=='宿舍灯坏了','missing location remains empty')
check(before==counts(),'unconfirmed draft no business write')
expect(400,submit(d),'missing required field rejected')
d2=turn(cid,'三号楼201')['draft'];check(d2['location']=='三号楼201' and d2['description']=='宿舍灯坏了','multi-turn context')
check('draft' not in turn(cid,'谢谢'),'small talk does not replace draft')
check('draft' not in turn(cid,'讲个校园笑话'),'topic switch with unchanged fields does not publish a replacement')
check(expect(200,req('GET',path(d2),user),'prior card after topic switch')['draft']['status']=='active','topic switch preserves editable draft')
expect(409,submit(d,location='三号楼201'),'superseded card rejected')
expect(409,save(d),'superseded edit rejected')
for token,label in ((second,'same-org other'),(other,'cross-org')):
 expect(404,req('GET',path(d2),token),label+' draft read')
 expect(404,req('GET',f'/api/conversations/{cid}/messages',token),label+' history')
 expect(404,save(d2,token),label+' edit')
 expect(404,submit(d2,token),label+' submit')
expect(401,req('GET',path(d2)),'anonymous draft read')
for token in (admin,tech):
 expect(403,submit(d2,token),'wrong role create')
 expect(403,save(d2,token),'wrong role edit')
expect(400,save(d2,category='unknown'),'unknown category')
expect(400,save(d2,location='房'*201),'location bound')
expect(400,save(d2,description='灯'*2001),'description bound')
expect(409,save(d2,version=0),'stale version')
unchanged=expect(200,save(d2),'same-content save')['draft'];check(unchanged['idempotency_key']==d2['idempotency_key'] and unchanged['version']==d2['version'],'same content keeps key/version')
edited=expect(200,save(d2,description='灯完全不亮'),'edit description')['draft'];check(edited['idempotency_key']!=d2['idempotency_key'] and edited['version']==d2['version']+1,'edit rotates key/version')
check(next(m['draft'] for m in history(cid) if m['id']==edited['message_id'])==edited,'saved edit history restoration')
expect(409,submit(d2),'old key/version rejected')
expect(409,submit(edited,location='伪造位置'),'body must match saved draft')
before=counts()
with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:results=list(pool.map(lambda _:submit(edited,org_id=2,requester_id=99999,status="closed"),range(5)))
check(sorted(code for code,_ in results)==[200,200,200,200,201],'five concurrent confirmations')
orders=[v['order'] for _,v in results];order=orders[0]
check(len({o['id'] for o in orders})==1,'one real order for retries')
check(order['org_id']==1 and order['requester_id']!=99999,'client identity ignored')
check(order['status']=='submitted' and order['number'].startswith('RP') and order['description']=='灯完全不亮','real order response')
check(sql(f"SELECT CONCAT((SELECT COUNT(*) FROM work_order_events WHERE work_order_id={order['id']}),':',(SELECT COUNT(*) FROM audit_events WHERE action='work_order.create' AND resource_id={order['id']}))")=='1:1','one event and create audit')
expect(200,req('GET',f"/api/work-orders/{order['id']}",user),'real detail')
expect(404,req('GET',f"/api/work-orders/{order['id']}",second),'detail belongs to requester')
submitted=expect(200,req('GET',path(edited),user),'submitted draft')['draft'];check(submitted['order_number']==order['number'] and submitted['order_id']==order['id'],'persisted real result')
expect(409,save(submitted),'submitted immutable')
sql("DELETE FROM idempotency_records WHERE idem_key='"+edited['idempotency_key']+"'")
check(expect(200,submit(edited),'replay after idempotency expiration')['order']['id']==order['id'],'expired key no duplicate')
new=turn(cid,'水管漏水，帮我报修')['draft'];check(new['category']=='plumbing' and new['location']=='','new request excludes previous submitted location')
cancelled=expect(200,save(new,status='cancelled'),'cancel')['draft'];before=counts();expect(409,submit(cancelled,location='三号楼201'),'cancelled cannot submit');check(before==counts(),'cancel no business write')
# A simultaneous edit and confirmation must serialize on the conversation/draft.
raced=complete()
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
 edit_future=pool.submit(save,raced,description='灯完全不亮')
 create_future=pool.submit(submit,raced)
 edit_result,create_result=edit_future.result(),create_future.result()
check((edit_result[0],create_result[0]) in ((200,409),(409,201)),'edit/submit race has one winner')
if edit_result[0]==200:expect(201,submit(edit_result[1]['draft']),'submit edited winner')
else:check(create_result[1]['order']['description']==raced['description'],'created original is immutable during race')
# Audit insert failures must roll back BOTH business and draft lifecycle.
d=complete();before=counts()
sql("CREATE TRIGGER draft_check_fail_audit BEFORE INSERT ON audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated test failure'")
try:expect(503,submit(d),'audit fault')
finally:sql('DROP TRIGGER draft_check_fail_audit')
check(before==counts(),'fault rolls back business')
check(expect(200,req('GET',path(d),user),'fault draft')['draft']['status']=='active','fault preserves draft')
expect(201,submit(d),'same draft retry after fault')
# No partial/invalid/identity-bearing model output may replace a good draft.
d=complete();cid=d['conversation_id'];before=counts()
for fault in ('draft-invalid','draft-unknown','draft-hallucinated','malformed','tools','fail','disconnect','timeout'):
 mode(fault);result=turn(cid,'灯完全不亮',False);check('draft' not in result,fault+' no published card')
 check(expect(200,req('GET',path(d),user),fault+' original draft')['draft']['status']=='active',fault+' retains previous')
 check(before==counts(),fault+' no business write')
mode('slow')
res=stream(cid,'灯完全不亮');check(event(res)[0]=='meta','slow extraction starts')
expect(409,save(d),'generation blocks edits');expect(409,submit(d),'generation blocks submit');res.close()
mode('ok');time.sleep(2.3)
check(expect(200,req('GET',path(d),user),'after stop')['draft']['status']=='active','cancelled stream keeps prior draft')
check(not any(m.get('draft') for m in history(cid) if m['status']!='complete'),'failed/cancelled messages have no card')
# Disable after draft creation; history remains private and readable.
config=next(v for v in expect(200,req('GET','/api/agents?manage=1',admin),'manage')['items'] if v['id']==aid)
disabled=expect(200,req('PUT',f'/api/agents/{aid}',admin,{**config,'enabled':False}),'disable')['agent']
try:expect(409,submit(d),'disabled assistant blocks confirmation');expect(409,save(d),'disabled assistant blocks edits')
finally:expect(200,req('PUT',f'/api/agents/{aid}',admin,{**disabled,'enabled':True}),'enable')
# Finish-time configuration check also guards draft publication.
mode('slow');res=stream(cid,'灯完全不亮');check(event(res)[0]=='meta','configuration race starts')
config=next(v for v in expect(200,req('GET','/api/agents?manage=1',admin),'config race read')['items'] if v['id']==aid)
expect(200,req('PUT',f'/api/agents/{aid}',admin,config),'change version during extraction')
events=[]
while True:
 e=event(res)
 if e is None:break
 events.append(e)
res.close()
check(events[-1][0]=='error' and not any(e[1].get('draft') for e in events),'configuration race publishes no card')
check(expect(200,req('GET',path(d),user),'config race prior')['draft']['status']=='active','configuration race keeps prior draft')
mode('ok')
# Ordinary container recreation must keep the saved card and result.
mode('ok');dc('restart','api')
for _ in range(100):
 try:
  if req('GET','/health/ready')[0]==200:break
 except Exception:pass
 time.sleep(.2)
check(expect(200,req('GET',path(submitted),user),'restart result')['draft']['order_id']==order['id'],'restart keeps submitted result')
check(expect(200,req('GET',path(d),user),'restart active')['draft']['status']=='active','restart keeps active draft')
(root/'tmp/draft-check-results.json').write_text(json.dumps({'checks':checks,'count':len(checks),'project':a.project},ensure_ascii=False,indent=2))
print(f'{len(checks)} checks passed; isolated mock evidence only.')
