"""Manual-flow integration tests. No passwords, tokens or response bodies are printed."""
import argparse,json,urllib.request,urllib.error,uuid,concurrent.futures,subprocess,time
from pathlib import Path
parser=argparse.ArgumentParser();parser.add_argument('--env-file',default='.env');parser.add_argument('--project',default='');args=parser.parse_args()
root=Path(__file__).resolve().parents[1]
env=dict(line.split('=',1) for line in (root/args.env_file).read_text().splitlines() if '=' in line and not line.startswith('#'))
base='http://127.0.0.1:'+env.get('WEB_PORT','18083')
checks=0

def request(method,path,data=None,token=None,headers=None):
 hs={'Content-Type':'application/json',**(headers or {})}
 if token:hs['Authorization']='Bearer '+token
 req=urllib.request.Request(base+path,data=json.dumps(data).encode() if data is not None else None,headers=hs,method=method)
 try:
  with urllib.request.urlopen(req,timeout=20) as res:return res.status,json.load(res),res.headers
 except urllib.error.HTTPError as res:
  try:body=json.load(res)
  except Exception:body={}
  return res.code,body,res.headers

def expect(code,result,name):
 global checks
 assert result[0]==code,f'{name}: expected {code}, got {result[0]}'
 checks+=1
 return result[1]

def login(name,key):
 res=request('POST','/api/login',{'username':name,'password':env[key]})
 v=expect(200,res,'login '+name);assert not res[2].get('Set-Cookie');return v['access_token'],v['user']

def create(token,data=None,key=None):
 payload=data or {'category':'lighting','location':'演示三号楼201','description':'灯不亮，需处理'}
 v=expect(201,request('POST','/api/work-orders',payload,token,{'Idempotency-Key':key or str(uuid.uuid4())}),'create')
 return v['order']

def detail(token,o):return expect(200,request('GET',f"/api/work-orders/{o['id']}",token=token),'detail')
def act(token,o,action,code=200,**fields):
 res=request('POST',f"/api/work-orders/{o['id']}/{action}",{'version':o['version'],**fields},token)
 v=expect(code,res,action)
 return v.get('order',o)

def sql(statement):
 if not args.project or not args.project.endswith('-check'):raise RuntimeError('Database fault tests require an isolated *-check project')
 cmd=['docker','compose','-p',args.project,'--env-file',args.env_file,'-f','compose.yaml','-f','compose.verify.yaml','exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" --batch --skip-column-names']
 p=subprocess.run(cmd,cwd=root,input=statement,text=True,capture_output=True)
 if p.returncode:raise RuntimeError('Isolated database test operation failed')
 return p.stdout.strip()

for attempt in range(60):
 try:
  if request('GET','/health/ready')[0]==200:break
 except Exception:pass
 time.sleep(.5)
else:raise RuntimeError('API readiness did not recover before verification')
expect(200,request('GET','/health/live'),'liveness');expect(200,request('GET','/health/ready'),'readiness')
expect(401,request('GET','/api/me'),'anonymous');expect(401,request('GET','/api/me',headers={'Cookie':'session=fake'}),'cookie only');expect(401,request('GET','/api/me',token='0'*64),'fake token')
req,ru=login('requester','SEED_REQUESTER_PASSWORD');req2,_=login('requester2','SEED_REQUESTER2_PASSWORD');admin,au=login('admin','SEED_ADMIN_PASSWORD');tech,tu=login('tech','SEED_TECH_PASSWORD');tech2,t2u=login('tech2','SEED_TECH2_PASSWORD');other,ou=login('other','SEED_OTHER_PASSWORD');otheradmin,oau=login('other_admin','SEED_OTHER_PASSWORD')
expect(403,request('POST','/api/work-orders',{'role':'requester'},admin),'admin cannot create')
for invalid in [{'category':'lighting','location':' ','description':'x'},{'category':'bad','location':'x','description':'x'},{'category':'lighting','location':'x','description':' '},{'category':'lighting','location':'x'*201,'description':'x'},{'category':'lighting','location':'x','description':'x'*2001}]:expect(400,request('POST','/api/work-orders',invalid,req,{'Idempotency-Key':str(uuid.uuid4())}),'invalid submission')
key=str(uuid.uuid4());payload={'category':'lighting','location':'演示三号楼201','description':'灯不亮，需处理','org_id':2,'requester_id':ru['id']+100,'status':'closed'}
o=create(req,payload,key);assert o['org_id']==ru['org_id'] and o['requester_id']==ru['id'] and o['status']=='submitted'
replay=expect(200,request('POST','/api/work-orders',payload,req,{'Idempotency-Key':key}),'idempotent replay');assert replay['order']['id']==o['id']
expect(409,request('POST','/api/work-orders',{**payload,'description':'changed'},req,{'Idempotency-Key':key}),'idempotency payload conflict')
ckey=str(uuid.uuid4())
with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:results=list(pool.map(lambda _:request('POST','/api/work-orders',payload,req,{'Idempotency-Key':ckey}),range(5)))
assert all(r[0] in (200,201) for r in results),'concurrent creation response';assert len({r[1]['order']['id'] for r in results})==1,'concurrent duplicates';checks+=1
for t in [req2,other,otheradmin,tech2]:expect(404,request('GET',f"/api/work-orders/{o['id']}",token=t),'invisible order')
for t in [req2,other,tech2]:v=expect(200,request('GET','/api/work-orders',token=t),'filtered list');assert o['id'] not in [x['id'] for x in v['items']]
act(req,o,'assign',403,technician_id=tu['id'],role='admin',org_id=1)
act(admin,o,'review',400,decision='reject',reason=' ')
o=act(admin,o,'review',decision='request_info',reason='请补充具体位置');assert o['status']=='needs_info'
o=act(req,o,'resubmit',category='lighting',location='三号楼201靠窗灯',description='无法点亮');assert o['status']=='submitted'
o=act(admin,o,'review',decision='approve');assert o['status']=='unassigned'
act(admin,o,'assign',400,technician_id=ru['id'])
act(admin,o,'assign',400,technician_id=oau['id'])
if args.project:
 sql(f"UPDATE users SET enabled=FALSE WHERE id={tu['id']};")
 try:act(admin,o,'assign',400,technician_id=tu['id'])
 finally:sql(f"UPDATE users SET enabled=TRUE WHERE id={tu['id']};")
 sql(f"DELETE FROM technician_categories WHERE user_id={t2u['id']} AND category='lighting';")
 try:act(admin,o,'assign',400,technician_id=t2u['id'])
 finally:sql(f"INSERT INTO technician_categories VALUES({t2u['id']},'lighting');")
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:results=list(pool.map(lambda uid:request('POST',f"/api/work-orders/{o['id']}/assign",{'version':o['version'],'technician_id':uid},admin),[tu['id'],t2u['id']]))
assert sorted(r[0] for r in results)==[200,409],'concurrent assignment';checks+=1
o=detail(admin,o)['order'];old=tech if o['assignee_id']==tu['id'] else tech2;new=tech2 if old==tech else tech;newid=t2u['id'] if new==tech2 else tu['id']
o=act(admin,o,'reassign',technician_id=newid,reason='调配任务');expect(404,request('GET',f"/api/work-orders/{o['id']}",token=old),'original assignee loses access')
act(admin,o,'accept',403);o=act(new,o,'accept');assert o['status']=='in_progress'
act(req,o,'cancel',409,reason='取消');act(admin,o,'reassign',409,technician_id=newid,reason='调配')
act(new,o,'complete',400,measures='',result='正常')
o=act(new,o,'records',measures='检查灯具',result='等待更换');o=act(new,o,'complete',measures='更换灯具',result='恢复照明');assert o['status']=='awaiting_confirmation'
act(req,o,'reopen',400,reason=' ');o=act(req,o,'reopen',reason='仍有闪烁');assert o['status']=='unassigned' and o['assignee_id'] is None
expect(404,request('GET',f"/api/work-orders/{o['id']}",token=new),'reopen clears assignment');assert len(detail(req,o)['records'])==2
o=act(admin,o,'assign',technician_id=tu['id']);o=act(tech,o,'accept');o=act(tech,o,'complete',measures='排查并更换',result='恢复稳定');o=act(req,o,'confirm');assert o['status']=='closed';act(req,o,'cancel',409,reason='取消')
assert len(detail(req,o)['events'])>=12
for st in ['submitted','needs_info','unassigned','assigned']:
 c=create(req)
 if st=='needs_info':c=act(admin,c,'review',decision='request_info',reason='请补充')
 if st in ['unassigned','assigned']:c=act(admin,c,'review',decision='approve')
 if st=='assigned':c=act(admin,c,'assign',technician_id=tu['id'])
 c=act(req,c,'cancel',reason='重复报修');assert c['status']=='cancelled'
c=create(req);c=act(admin,c,'review',decision='reject',reason='无效报修');assert c['status']=='rejected';act(admin,c,'review',409,decision='approve')
expect(403,request('GET','/api/technicians?category=lighting',token=req),'staff privacy')
v=expect(200,request('GET','/api/technicians?category=lighting',token=admin),'staff query');assert len(v['items'])==2
expect(403,request('GET','/api/audit',token=req),'audit privacy');v=expect(200,request('GET',f"/api/audit?resource_id={o['id']}",token=admin),'audit');assert any(x['action']=='work_order.confirm' for x in v['items'])
cap=expect(200,request('GET','/api/capabilities',token=req),'capabilities');assert isinstance(cap['assistant'],bool) and cap['mcp'] is False
if args.project:
 c=create(req);before=detail(req,c)
 sql("CREATE TRIGGER audit_reject BEFORE INSERT ON audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated test failure';")
 try:act(admin,c,'review',503,decision='approve')
 finally:sql('DROP TRIGGER audit_reject;')
 after=detail(req,c);assert after['order']['version']==before['order']['version'] and len(after['events'])==len(before['events']);checks+=1
 sql(f"UPDATE sessions SET expires_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE user_id={ru['id']};")
 expect(401,request('GET','/api/me',token=req),'expired token');req,ru=login('requester','SEED_REQUESTER_PASSWORD')
 sql(f"UPDATE users SET enabled=FALSE WHERE id={ru['id']};")
 try:expect(401,request('GET','/api/me',token=req),'disabled user')
 finally:sql(f"UPDATE users SET enabled=TRUE WHERE id={ru['id']};")
 req,ru=login('requester','SEED_REQUESTER_PASSWORD')
 sql(f"UPDATE users SET role='unknown' WHERE id={oau['id']};")
 try:expect(401,request('GET','/api/me',token=otheradmin),'invalid role rejected')
 finally:sql(f"UPDATE users SET role='admin' WHERE id={oau['id']};")
expect(200,request('POST','/api/logout',token=req),'logout');expect(401,request('GET','/api/me',token=req),'revoked token')
for n in range(5):expect(401,request('POST','/api/login',{'username':'missing','password':'invalid'}),'failed login')
expect(429,request('POST','/api/login',{'username':'missing','password':'invalid'}),'login rate limit')
print(f'PASS: {checks} API checks; state flow, isolation, retries, concurrency and token lifecycle verified. No credentials printed.')
