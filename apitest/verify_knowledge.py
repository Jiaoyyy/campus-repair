"""Knowledge integration and fault checks, restricted to isolated *-check environment."""
import argparse,json,urllib.request,urllib.error,uuid,subprocess,time
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--project',default='campus-repair-check');p.add_argument('--env-file',default='tmp/check.env');a=p.parse_args()
if not a.project.endswith('-check'):raise SystemExit('Requires isolated *-check project')
root=Path(__file__).resolve().parents[1];env=dict(l.split('=',1) for l in (root/a.env_file).read_text().splitlines() if '=' in l and not l.startswith('#'));base='http://127.0.0.1:'+env['WEB_PORT'];checks=0
compose=['docker','compose','-p',a.project,'--env-file',a.env_file,'-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml']
def dc(*cmd):
 r=subprocess.run(compose+list(cmd),cwd=root,capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Isolated container operation failed: '+cmd[0])
 return r.stdout

def sql(q):
 r=subprocess.run(compose+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" --batch --skip-column-names'],cwd=root,input=q,capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Isolated database test operation failed')
 return r.stdout.strip()
def req(method,path,token=None,data=None,ctype='application/json',raw=False):
 hs={'Content-Type':ctype}
 if token:hs['Authorization']='Bearer '+token
 if isinstance(data,dict):data=json.dumps(data).encode()
 try:
  with urllib.request.urlopen(urllib.request.Request(base+path,data=data,headers=hs,method=method),timeout=30) as r:
   b=r.read();return r.status,b if raw else json.loads(b)
 except urllib.error.HTTPError as r:
  try:return r.code,json.load(r)
  except:return r.code,{}
def check(v,name):
 global checks
 assert v,name;checks+=1
def expect(code,result,name):check(result[0]==code,name+': HTTP '+str(result[0]));return result[1]
def login(user,key):return expect(200,req('POST','/api/login',data={'username':user,'password':env[key]}),'login')['access_token']
def upload(token,name='demo.md',body=None,title='演示照明报修须知',extra=''):
 if body is None:body=('照明报修须填写楼栋、房间和故障现象。\n注意安全，发现冒烟应停止使用，联系现场工作人员。\n'+extra).encode()
 boundary='test'+uuid.uuid4().hex
 data=(f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n{title}\r\n--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\nContent-Type: text/plain\r\n\r\n').encode()+body+f'\r\n--{boundary}--\r\n'.encode()
 return req('POST','/api/materials',token,data,'multipart/form-data; boundary='+boundary)
def detail(id):return expect(200,req('GET',f'/api/materials/{id}',admin),'material')['material']
def wait(id,status):
 for _ in range(120):
  m=detail(id)
  if m['status']==status:return m
  time.sleep(.5)
 raise AssertionError('Index did not reach '+status)
def search(mode='keyword',query='照明报修',token=None):return req('GET','/api/retrieval?mode='+mode+'&q='+urllib.parse.quote(query),token or admin)
state=root/'tmp/knowledge-mock/mode';state.write_text('ok')
for attempt in range(80):
 try:
  with urllib.request.urlopen(base+'/health/ready',timeout=2) as readiness:
   if readiness.status==200:break
 except Exception:pass
 time.sleep(.25)
else:raise RuntimeError('Isolated API did not become ready')
admin=login('admin','SEED_ADMIN_PASSWORD');requester=login('requester','SEED_REQUESTER_PASSWORD');tech=login('tech','SEED_TECH_PASSWORD');other=login('other_admin','SEED_OTHER_PASSWORD')
expect(401,req('GET','/api/materials'),'anonymous')
for token in (requester,tech):
 expect(403,upload(token),'nonadmin upload');expect(403,req('GET','/api/materials',token),'nonadmin management');expect(403,search(token=token),'nonadmin direct retrieval')
for name,body,code in [('x.exe',b'abc',400),('x.txt',b'\xff',400),('x.md',b' \n',400),('x.md',b'\xef\xbb\xbf \n',400),('x.txt',b'x'*(10*1024*1024+1),413)]:expect(code,upload(admin,name,body),'upload validation')
body=('照明报修💡原文保持。\n'+'安全报修须知。'*140).encode()
id=expect(201,upload(admin,body=body),'upload')['id'];m=wait(id,'ready');check(m['generation']==1 and m['vector_ready'],'first publication');check(m['text'].encode()==body,'original unchanged')
check(expect(200,req('GET',f'/api/materials/{id}/file',admin,raw=True),'download')==body,'original file exact')
for suffix in ('','/file'):
 expect(404,req('GET',f'/api/materials/{id}{suffix}',other),'cross organization')
expect(404,req('POST',f'/api/materials/{id}/reindex',other),'cross reindex')
for mode in ('keyword','vector','hybrid'):
 hits=expect(200,search(mode),'retrieval '+mode)['items'];check(bool(hits),'retrieval result '+mode)
 for h in hits:
  src=detail(h['material_id']);check(h['text']==src['text'][h['start']:h['end']] and h['generation']==src['generation'],'range exact')
expect(200,search(query='不存在的鲸鱼政策'),'no match');check(not expect(200,search(query='鲸鱼星际航线'),'no match')['items'],'no match empty')
foreign_hits=expect(200,search(token=other),'other org scoped')['items'];check(all(sql(f"SELECT org_id FROM knowledge_materials WHERE id={h['material_id']}")=='2' for h in foreign_hits),'cross org retrieval scoped')
expect(400,req('GET','/api/retrieval?mode=bad&q=x',admin),'invalid mode');expect(400,req('GET','/api/retrieval?agent_id=invalid&q=x',admin),'invalid agent rejected')
# Failed rebuild keeps published generation and source bytes.
state.write_text('fail');expect(202,req('POST',f'/api/materials/{id}/reindex',admin),'reindex');m=wait(id,'failed');check(m['generation']==1 and m['vector_ready'],'old generation retained')
id2=expect(201,upload(admin,extra='首次嵌入失败测试'),'failure upload')['id'];m2=wait(id2,'failed');check(m2['generation']==1 and not m2['vector_ready'],'first keyword only');check(m2['text'],'failure retains original')
expect(200,search('keyword'),'keyword independent');expect(503,search('vector'),'vector not fake empty');expect(503,search('hybrid'),'hybrid not fake empty')
check(expect(200,req('GET',f'/api/materials/{id2}/file',admin,raw=True),'failed download').startswith('照明'.encode()),'failed original preserved')
state.write_text('ok');expect(202,req('POST',f'/api/materials/{id2}/reindex',admin),'retry failure');wait(id2,'ready');expect(202,req('POST',f'/api/materials/{id}/reindex',admin),'retry rebuild');m=wait(id,'ready');check(m['generation']==3,'monotonic generation');hits=expect(200,search('hybrid'),'rebuilt retrieval')['items'];check(all(h['generation']==detail(h['material_id'])['generation'] for h in hits),'no mixed generations')
# Real process crash; task is recovered only after its persisted lease expires.
state.write_text('slow');id3=expect(201,upload(admin,extra='崩溃恢复演示'),'crash upload')['id'];wait(id3,'processing')
expect(409,req('POST',f'/api/materials/{id3}/reindex',admin),'duplicate active task')
for _ in range(40):
 if sql(f'SELECT COUNT(*) FROM knowledge_chunks WHERE material_id={id3}')!='0':break
 time.sleep(.25)
time.sleep(6);check(int(sql(f'SELECT TIMESTAMPDIFF(SECOND,created_at,lease_until) FROM index_jobs WHERE material_id={id3}'))>=34,'lease renewed')
dc('kill','-s','SIGKILL','worker');state.write_text('ok');dc('start','worker');m3=wait(id3,'ready');check(m3['generation']==1,'crash single publication');check(int(sql(f'SELECT attempts FROM index_jobs WHERE material_id={id3}'))>=2,'lease reclaimed');check(sql(f'SELECT COUNT(*) FROM index_jobs WHERE material_id={id3}')=='1','single durable job');check(sql(f'SELECT COUNT(*) FROM audit_events WHERE action="knowledge.index" AND resource_id={id3}')=='1','single publish audit')
# Old/failed vectors are eventually removed via durable cleanup jobs.
for _ in range(30):
 if sql('SELECT COUNT(*) FROM vector_cleanup_jobs')=='0':break
 time.sleep(.5)
check(sql('SELECT COUNT(*) FROM vector_cleanup_jobs')=='0','cleanup tasks complete')
# Qdrant contains no text and old generations were actually deleted.
import hashlib
collection='check_v1_'+hashlib.sha256(b'mock-4:4').hexdigest()[:16]
def vector_http(method,path,data=None):
 code='import urllib.request,json; r=urllib.request.Request('+repr('http://qdrant:6333'+path)+',data='+repr(json.dumps(data).encode() if data is not None else None)+',headers={"Content-Type":"application/json"},method='+repr(method)+'); print(urllib.request.urlopen(r).read().decode())'
 return json.loads(dc('exec','-T','embedding-mock','python','-c',code))
for mid in (id,id2):
 points=vector_http('POST','/collections/'+collection+'/points/scroll',{'limit':100,'with_payload':True,'filter':{'must':[{'key':'material_id','match':{'value':mid}}]}})['result']['points']
 check(bool(points),'published vectors exist');check(all(set(p['payload'])=={'org_id','material_id','generation'} for p in points),'no vector text payload');check({p['payload']['generation'] for p in points}=={detail(mid)['generation']},'old vectors removed')
# A stale point cannot become evidence even if Qdrant returns it.
stale=int(sql(f"INSERT INTO knowledge_chunks(material_id,generation,sequence_no,start_char,end_char,body) VALUES({id},999999,1,0,2,'旧文'); SELECT LAST_INSERT_ID();"))
vector_http('PUT','/collections/'+collection+'/points?wait=true',{'points':[{'id':stale,'vector':[1,1,1,1],'payload':{'org_id':1,'material_id':id,'generation':999999}}]})
check(all(h['chunk_id']!=stale for h in expect(200,search('hybrid'),'stale candidate')['items']),'stale candidate rejected')
vector_http('POST','/collections/'+collection+'/points/delete?wait=true',{'points':[stale]})
# A Qdrant outage is explicit and does not affect keyword search or manual readiness.
dc('stop','qdrant');expect(503,search('vector'),'Qdrant outage');expect(503,search('hybrid'),'hybrid outage');expect(200,search('keyword'),'keyword during vector outage');expect(200,req('GET','/health/ready'),'manual ready during vector outage');dc('start','qdrant')
# Dependency failure is explicit even with all existing vectors published.
state.write_text('fail');expect(503,search('vector'),'embedding outage');expect(200,search('keyword'),'keyword during embedding outage');state.write_text('ok')
# Original and task state survive API/worker recreation; never remove volumes.
dc('up','-d','--no-build','--force-recreate','api','worker');
for _ in range(60):
 try:
  if req('GET','/health/ready')[0]==200:break
 except Exception:pass
 time.sleep(.5)
check(detail(id)['text'].encode()==body,'recreation preserves text');check(expect(200,req('GET',f'/api/materials/{id}/file',admin,raw=True),'persistent download')==body,'recreation preserves file')
# Exact upload cap. Worker stopped to keep this boundary test out of embedding load.
dc('stop','worker');big=expect(201,upload(admin,body=b'x'*(10*1024*1024),title='演示大小边界'),'exact 10MiB')['id'];check(detail(big)['status']=='pending','durable pending');dc('start','worker')
print(f'PASS: {checks} knowledge assertions (mock embedding, real MySQL/Qdrant, isolated crash/recovery).')
