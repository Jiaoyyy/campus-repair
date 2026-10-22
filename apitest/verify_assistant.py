"""Role-bound RAG, citations and private streaming conversations, isolated only."""
import argparse,json,urllib.request,urllib.error,urllib.parse,uuid,subprocess,time,concurrent.futures
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--project',default='campus-repair-check');p.add_argument('--env-file',default='tmp/check.env');args=p.parse_args()
if not args.project.endswith('-check'):raise SystemExit('Requires isolated *-check project')
root=Path(__file__).resolve().parents[1];env=dict(l.split('=',1) for l in (root/args.env_file).read_text().splitlines() if '=' in l and not l.startswith('#'));base='http://127.0.0.1:'+env['WEB_PORT'];checks=0
compose=['docker','compose','-p',args.project,'--env-file',args.env_file,'-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml']
state=root/'tmp/knowledge-mock';state.mkdir(exist_ok=True);(state/'mode').write_text('ok');(state/'chat-mode').write_text('ok');(state/'chat-requests.jsonl').write_text('')
def mode(v):dc('exec','-T','chat-mock','python','-c',"from pathlib import Path; Path('/state/chat-mode').write_text("+repr(v)+")")
def calls():return [json.loads(l) for l in dc('exec','-T','chat-mock','cat','/state/chat-requests.jsonl').splitlines() if l]
def dc(*cmd):
 r=subprocess.run(compose+list(cmd),cwd=root,capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Isolated container operation failed')
 return r.stdout

def sql(q):
 r=subprocess.run(compose+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" --batch --skip-column-names'],cwd=root,input=q,capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Isolated SQL operation failed')
 return r.stdout.strip()
def check(v,name):
 global checks
 assert v,name;checks+=1
def expect(code,result,name):check(result[0]==code,name+': HTTP '+str(result[0]));return result[1]
def req(method,path,token=None,data=None,ctype='application/json'):
 hs={'Content-Type':ctype}
 if token:hs['Authorization']='Bearer '+token
 if isinstance(data,dict):data=json.dumps(data).encode()
 try:
  with urllib.request.urlopen(urllib.request.Request(base+path,data=data,headers=hs,method=method),timeout=15) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as r:return r.code,json.load(r)
def login(name,key):return expect(200,req('POST','/api/login',data={'username':name,'password':env[key]}),'login')['access_token']
def upload(token,title,text):
 boundary='test'+uuid.uuid4().hex
 body=(f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n{title}\r\n--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="demo.md"\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
 id=expect(201,req('POST','/api/materials',token,body,'multipart/form-data; boundary='+boundary),'upload')['id']
 for _ in range(80):
  m=expect(200,req('GET',f'/api/materials/{id}',token),'index wait')['material']
  if m['status']=='ready':return id
  time.sleep(.25)
 raise AssertionError('Index did not become ready')
def configure(id,**fields):
 a=next(a for a in expect(200,req('GET','/api/agents?manage=1',admin),'configuration')['items'] if a['id']==id)
 a.update(fields);return expect(200,req('PUT',f'/api/agents/{id}',admin,a),'save configuration')['agent']
def create(agent,token=None):return expect(201,req('POST','/api/conversations',token or requester,{'agent_id':agent}),'create conversation')['conversation']['id']
def messages(c,token=None):return expect(200,req('GET',f'/api/conversations/{c}/messages',token or requester),'history')['items']
def open_stream(c,text='照明报修需要什么信息？',token=None,mode='keyword'):
 return urllib.request.urlopen(urllib.request.Request(base+f'/api/conversations/{c}/messages',data=json.dumps({'content':text,'mode':mode}).encode(),headers={'Authorization':'Bearer '+(token or requester),'Content-Type':'application/json'},method='POST'),timeout=15)
def event(res):
 ev='';parts=[]
 while True:
  line=res.readline()
  if not line:return None
  line=line.decode().rstrip('\r\n')
  if line.startswith('event:'):ev=line[6:].strip()
  if line.startswith('data:'):parts.append(line[5:].strip())
  if not line and ev:return ev,json.loads(''.join(parts))
def consume(res):
 events=[]
 while True:
  e=event(res)
  if e is None:break
  events.append(e)
 res.close();return events

def turn(c,text='照明报修需要什么信息？',token=None):
 events=consume(open_stream(c,text,token));check(events[0][0]=='meta','meta first');check(sum(e[0] in ('done','error') for e in events)==1,'one terminal event');return events
for attempt in range(80):
 try:
  with urllib.request.urlopen(base+'/health/ready',timeout=2) as readiness:
   if readiness.status==200:break
 except Exception:pass
 time.sleep(.25)
else:raise RuntimeError('Isolated API did not become ready')
admin=login('admin','SEED_ADMIN_PASSWORD');requester=login('requester','SEED_REQUESTER_PASSWORD');requester2=login('requester2','SEED_REQUESTER2_PASSWORD');tech=login('tech','SEED_TECH_PASSWORD');other=login('other','SEED_OTHER_PASSWORD');otheradmin=login('other_admin','SEED_OTHER_PASSWORD')
agents=expect(200,req('GET','/api/agents?manage=1',admin),'manage agents')['items'];a=next(x for x in agents if x['role']=='requester');ta=next(x for x in agents if x['role']=='technician');aa=next(x for x in agents if x['role']=='admin');aid=a['id']
for token,role in ((requester,'requester'),(tech,'technician'),(admin,'admin')):
 items=expect(200,req('GET','/api/agents',token),'role agents')['items'];check(all(x['role']==role and 'prompt' not in x for x in items),'role and private prompt')
expect(403,req('GET','/api/agents?manage=1',requester),'nonadmin management');expect(403,req('PUT',f'/api/agents/{aid}',requester,a),'nonadmin config');expect(404,req('POST','/api/conversations',requester,{'agent_id':ta['id']}),'wrong role');expect(404,req('PUT',f'/api/agents/{aid}',otheradmin,a),'cross config')
orders_before=sql("SELECT CONCAT(COUNT(*),':',COALESCE(SUM(version),0)) FROM work_orders")
m1=upload(admin,'演示：助手照明报修须知','照明报修请填写楼栋、房间、故障现象和发现时间。\n演示资料，不代表校方制度。')
m2=upload(admin,'演示：安全提醒','照明报修注意安全，发现冒烟请停止使用并联系现场人员。')
secret=upload(admin,'演示：未绑定材料','隧道量子梨花专用资料。不得出现在未绑定助手检索中。')
foreign=upload(otheradmin,'隔离演示资料','照明报修隔离组织资料。')
a=configure(aid,enabled=True,material_ids=[m1,m2]);configure(ta['id'],enabled=True,material_ids=[m1]);configure(aa['id'],enabled=True,material_ids=[m1])
expect(400,req('PUT',f'/api/agents/{aid}',admin,{**a,'material_ids':[foreign]}),'cross material binding');expect(409,req('PUT',f'/api/agents/{aid}',admin,{**a,'version':a['version']-1}),'stale configuration')
for search_mode in ('keyword','vector','hybrid'):
 hits=expect(200,req('GET',f'/api/retrieval?agent_id={aid}&mode={search_mode}&q='+urllib.parse.quote('照明报修'),requester),'bound search')['items'];check(bool(hits) and all(h['material_id'] in (m1,m2) for h in hits),'bound/current/org scope')
expect(403,req('GET','/api/retrieval?q=x',requester),'no direct management');expect(404,req('GET',f'/api/retrieval?agent_id={aid}&q=x',other),'cross agent');expect(404,req('GET',f'/api/retrieval?agent_id={aid}&q=x',tech),'role scope')
check(not expect(200,req('GET',f'/api/retrieval?agent_id={aid}&q='+urllib.parse.quote('隧道量子梨花'),requester),'unbound text')['items'],'unbound text absent')
c=create(aid);events=turn(c);done=events[-1][1];check(events[-1][0]=='done' and len(done['citations'])==1,'only actual used evidence cited');cite=done['citations'][0];mid=done['message_id'];source_path=f'/api/conversations/{c}/citations/{mid}/{cite["chunk_id"]}'
source=expect(200,req('GET',source_path,requester),'source');check(source['text'][cite['start']:cite['end']]==cite['text'],'original location exact')
for t in (requester2,admin,other):
 expect(404,req('GET',f'/api/conversations/{c}/messages',t),'private history');expect(404,req('GET',source_path,t),'private source');expect(404,req('POST',f'/api/conversations/{c}/messages',t,{'content':'x'}),'private send')
expect(404,req('GET',f'/api/conversations/{c}/citations/{mid}/9999999',requester),'fabricated source')
for text in (' ', '灯'*2001):expect(400,req('POST',f'/api/conversations/{c}/messages',requester,{'content':text}),'input bound')
mode('fake');events=turn(c);done=events[-1][1];check('[999]' not in done['answer'] and done['warning'] and len(done['citations'])==1,'invalid citation rejected');check(all('[999]' not in e[1].get('text','') for e in events),'invalid marker never streamed')
mode('unused');done=turn(c)[-1][1];check(not done['citations'] and done['warning'],'unused candidates are not references')
mode('unsafe');done=turn(c)[-1][1];check('第一步' not in done['answer'] and not done['citations'],'unsafe steps blocked')
mode('ok');before=len(calls());done=turn(c,'鲸鱼星际维修时限规定')[-1][1];check('无法确认' in done['answer'] and not done['citations'] and len(calls())==before+1,'model handles unverified policy without invented citation')
# Failed messages and inputs never enter the next completed-history context.
mode('disconnect');check(turn(c,'照明报修失败上下文不能用')[-1][0]=='error','interruption explicit');history=messages(c);check(history[-1]['status']=='failed' and history[-1]['content'],'partial persisted')
for m in ('fail','malformed','timeout','tools'):
 mode(m);check(turn(c)[-1][0]=='error','dependency/protocol failure '+m)
mode('ok')
for i in range(6):check(turn(c,'照明报修信息第'+str(i)+'轮')[-1][0]=='done','context round')
last=calls()[-1]['messages'];check(len(last)==13,'last ten complete messages plus two systems and input');check(not any('失败上下文不能用' in m['content'] for m in last[2:]),'failed context excluded')
# Concurrent generation is rejected; closing a client cancels and preserves safe partial text.
mode('slow');res=open_stream(c);check(event(res)[0]=='meta','concurrent meta');check(event(res)[0]=='delta','safe partial delta');expect(409,req('POST',f'/api/conversations/{c}/messages',requester,{'content':'照明报修'}),'same conversation generation lock');res.close()
for _ in range(40):
 h=messages(c)
 if h[-1]['status']!='generating':break
 time.sleep(.1)
check(h[-1]['status']=='cancelled' and h[-1]['content'],'cancel persisted and lock released')
# A revoked login cannot complete a round, even if the request started authenticated.
short=login('requester','SEED_REQUESTER_PASSWORD');res=open_stream(c,token=short);event(res);event(res);expect(200,req('POST','/api/logout',short),'revoke generating identity');check(consume(res)[-1][0]=='error','revoked identity no completed claim')
# Reconfiguration during a round prevents a completed claim and future model calls.
res=open_stream(c);event(res);event(res);configure(aid,enabled=False);ev=consume(res);check(ev[-1][0]=='error','disabled during generation');before=len(calls());expect(409,req('POST',f'/api/conversations/{c}/messages',requester,{'content':'照明报修'}),'disabled send');check(len(calls())==before,'disabled no model call');check(messages(c),'disabled retains own history');expect(404,req('GET',source_path,requester),'disabled original rejected')
configure(aid,enabled=True,material_ids=[]);check(not expect(200,req('GET',f'/api/retrieval?agent_id={aid}&q='+urllib.parse.quote('照明报修'),requester),'unbind')['items'],'unbind removes search');expect(404,req('GET',source_path,requester),'unbound citation denied');h=messages(c);check(any(m['warning'] and not m['citations'] for m in h if m['role']=='assistant' and m['status']=='complete'),'history revalidates citations')
mode('ok');general=create(aid);before=len(calls());hello=turn(general,'你好，你能做什么？')[-1][1];check('你好' in hello['answer'] and not hello['citations'] and len(calls())==before+1,'greeting reaches model without materials')
fault=turn(general,'宿舍灯坏了')[-1][1];check('房间' in fault['answer'] and not fault['citations'],'unbound fault asks missing information')
follow=turn(general,'三号楼201')[-1][1];check('201' in follow['answer'] and '哪栋' not in follow['answer'],'follow-up keeps provided location');check(any(m['content']=='宿舍灯坏了' for m in calls()[-1]['messages']),'model receives completed repair context')
configure(aid,enabled=True,material_ids=[m1,m2]);mode('ok');latest=turn(c)[-1][1];cite=latest['citations'][0];path=f'/api/conversations/{c}/citations/{latest["message_id"]}/{cite["chunk_id"]}'
# Automatic retrieval fails over visibly; unavailable evidence does not stop clarification.
(state/'mode').write_text('fail')
try:
 degraded=turn(create(aid))[-1][1];check(degraded['warning'] and degraded['citations'],'embedding failure falls back to authorized text evidence')
finally:(state/'mode').write_text('ok')
dc('stop','worker');sql('RENAME TABLE knowledge_chunks TO knowledge_chunks_temporarily_unavailable')
try:
 unavailable=turn(create(aid),'宿舍灯坏了')[-1][1];check('资料查询暂不可用' in unavailable['warning'] and not unavailable['citations'],'all search failures remain explicit while dialogue continues')
finally:
 sql('RENAME TABLE knowledge_chunks_temporarily_unavailable TO knowledge_chunks');dc('start','worker')
expect(202,req('POST',f'/api/materials/{cite["material_id"]}/reindex',admin),'reindex cited source')
for _ in range(80):
 m=expect(200,req('GET',f'/api/materials/{cite["material_id"]}',admin),'wait publish')['material']
 if m['generation']>cite['generation']:break
 time.sleep(.1)
expect(404,req('GET',path,requester),'old generation citation denied')
# Real process crash, then simulate lease expiry rather than waiting 130 seconds.
mode('slow');res=open_stream(c);event(res);event(res);dc('kill','-s','SIGKILL','api');res.close();dc('start','api')
for _ in range(60):
 try:
  if req('GET','/health/ready')[0]==200:break
 except Exception:pass
 time.sleep(.25)
sql(f'UPDATE conversations SET generation_until=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE id={c}')
h=messages(c);check(h[-1]['status']=='failed' and '中断' in h[-1]['warning'],'expired crash lock recovered');mode('ok');check(turn(c)[-1][0]=='done','new turn after crash')
# Tech/admin roles also get their own assistant, no operational writes exposed.
for agent,token in ((ta['id'],tech),(aa['id'],admin)):
 cid=create(agent,token);check(turn(cid,token=token)[-1][0]=='done','role assistant answer')
# Natural wording and category follow-up use the same bound, role-scoped MCP path.
configure(aid,tool_names=['get_my_work_orders']);natural=create(aid)
events=turn(natural,'我报的维修到哪一步了？');check(events[0][1]['tool']=='get_my_work_orders' and events[-1][0]=='done','natural requester progress uses MCP')
configure(aa['id'],tool_names=['list_dispatch_candidates']);dispatch=create(aa['id'],admin)
events=turn(dispatch,'我们有几个师傅？',admin);check(not events[0][1]['tool'],'missing category never queries invented all-staff count')
events=turn(dispatch,'照明的',admin);check(events[0][1]['tool']=='list_dispatch_candidates' and events[-1][0]=='done','category follow-up uses real authorized MCP')
configure(aid,tool_names=[]);events=turn(create(aid),'我报的维修到哪一步了？');check(events[-1][0]=='error','natural wording cannot bypass tool binding')
check(sql("SELECT CONCAT(COUNT(*),':',COALESCE(SUM(version),0)) FROM work_orders")==orders_before,'assistant and rejected tools never mutate work orders')
check(sql("SELECT COUNT(*) FROM audit_events WHERE metadata LIKE '%失败上下文不能用%'")=='0','no private dialogue in audit')
for secret_value in (env['SEED_ADMIN_PASSWORD'],env['SESSION_SECRET'],'isolated-chat-mock-only'):
 check(all(secret_value not in json.dumps(m,ensure_ascii=False) for req_body in calls() for m in req_body['messages']),'credentials absent from model messages')
# Save a convenient fictional session for browser QA without printing credentials.
(root/'tmp/assistant-fixture.json').write_text(json.dumps({'agent_id':aid,'material_ids':[m1,m2],'conversation_id':c}))
print(f'PASS: {checks} assistant assertions; mock model, real database/vector service; expiry after crash simulated explicitly.')
