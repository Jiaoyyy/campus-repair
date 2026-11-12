"""Read-only analysis integration in the dedicated fictional insights-check instance.
Never seeds, injects faults, or changes settings in the user's review environment.
"""
import concurrent.futures
import json
import subprocess
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
FOLDER=ROOT/'tmp/insights'
COMPOSE=['docker','compose','-p','campus-repair-insights-check','--project-directory',str(ROOT),'-f',str(FOLDER/'compose.json')]
BASE='http://127.0.0.1:18088'
checks=[]
def check(value,label):
 if not value:raise AssertionError(label)
 checks.append(label);print('PASS',label,flush=True)
def request(method,path,token=None,body=None):
 headers={'Content-Type':'application/json','Idempotency-Key':uuid.uuid4().hex}
 if token:headers['Authorization']='Bearer '+token
 req=urllib.request.Request(BASE+path,method=method,headers=headers,data=json.dumps(body).encode() if body is not None else None)
 with urllib.request.urlopen(req,timeout=20) as r:return json.load(r)
def sql(query):
 r=subprocess.run(COMPOSE+['exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_PASSWORD" mysql --default-character-set=utf8mb4 -u "$MYSQL_USER" "$MYSQL_DATABASE" -N -B'],input=query,capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Isolated SQL failed (private diagnostics suppressed)')
 return r.stdout.strip()
def events(token,cid,text,timeout=30):
 req=urllib.request.Request(BASE+f'/api/conversations/{cid}/messages',data=json.dumps({'content':text}).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+token})
 values=[];event='';data=[]
 with urllib.request.urlopen(req,timeout=timeout) as r:
  for line in r:
   line=line.decode().strip()
   if line.startswith('event:'):event=line[6:].strip()
   elif line.startswith('data:'):data.append(line[5:].strip())
   elif not line and event:values.append((event,json.loads(''.join(data))));event='';data=[]
 return values
def run():
 config=json.loads((FOLDER/'compose.json').read_text())
 for service in ('db','qdrant'):
  runtime=json.loads(subprocess.check_output(['docker','inspect','campus-repair-insights-check-'+service+'-1']))[0]
  check(all(v.get('Name','').startswith('campus-repair-insights-check_') for v in runtime['Mounts'] if v['Type']=='volume'),'runtime isolated volume '+service)
 check(config['name']=='campus-repair-insights-check' and config['services']['api']['environment']['CHAT_BASE_URL']=='http://chat-mock:8080/v1','guard isolated mock destination')
 env=dict(line.split('=',1) for line in (FOLDER/'check.env').read_text().splitlines() if '=' in line)
 tokens={};agents={}
 for name,key in [('admin','ADMIN'),('requester','REQUESTER'),('requester2','REQUESTER2'),('tech','TECH'),('tech2','TECH2'),('other','OTHER')]:
  tokens[name]=request('POST','/api/login',body={'username':name,'password':env['SEED_'+key+'_PASSWORD']})['access_token']
  agents[name]=request('GET','/api/agents',tokens[name])['items'][0]['id']
 def cid(name):return request('POST','/api/conversations',tokens[name],{'agent_id':agents[name]})['conversation']['id']
 def turn(name,text,conversation=None):
  c=conversation or cid(name);ev=events(tokens[name],c,text);check(ev[-1][0]=='done' and sum(k in ('done','error') for k,_ in ev)==1,name+' unique successful terminal')
  return c,ev[-1][1]
 mode=FOLDER/'mock/chat-mode';mode.write_text('insight')
 # This suite can be rerun: independent location namespace, old fixture rows are left intact.
 suffix=uuid.uuid4().hex[:8];location='分析验收楼'+suffix
 created=[]
 for i in range(12):
  name='requester' if i<8 else 'requester2';category='lighting' if i<7 else 'plumbing'
  o=request('POST','/api/work-orders',tokens[name],{'category':category,'location':location,'description':'走廊灯不亮' if i<7 else '水管漏水'})['order'];created.append(o)
 extra=request('POST','/api/work-orders',tokens['requester'],{'category':'plumbing','location':'多点验收区'+suffix,'description':'虚构其他位置'})['order']
 other=request('POST','/api/work-orders',tokens['other'],{'category':'lighting','location':location,'description':'外组织不可见'})['order']
 uid={name:int(sql("SELECT id FROM users WHERE username='"+name+"' LIMIT 1")) for name in ('tech','tech2')}
 # Fixtures model two no-shows on one old order, one old no-show outside the window,
 # and one order in two current exception queues. Actual input records stay fictional.
 ids=[int(o['id']) for o in created]
 sql(f"UPDATE work_orders SET status='in_progress',assignee_id={uid['tech']},created_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 14 DAY) WHERE id={ids[0]}; UPDATE work_orders SET assignee_id={uid['tech']},status='assigned' WHERE id={ids[1]};")
 for days in (0,1,20):sql(f"INSERT INTO work_order_visits(work_order_id,technician_id,starts_at,ends_at,stage,no_show_at,end_reason) VALUES({ids[0]},{uid['tech']},DATE_SUB(UTC_TIMESTAMP(6),INTERVAL {days+1} DAY),DATE_SUB(UTC_TIMESTAMP(6),INTERVAL {days+1} DAY),'no_show',DATE_SUB(UTC_TIMESTAMP(6),INTERVAL {days} DAY),'虚构白跑');")
 sql(f"INSERT INTO work_order_visits(work_order_id,technician_id,starts_at,ends_at,stage,end_reason) VALUES({ids[0]},{uid['tech']},DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 2 DAY),DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 DAY),'scheduled',''); SET @visit=LAST_INSERT_ID(); INSERT INTO work_order_visit_context(work_order_id,active_visit_id,exception,reason,expected_resume_at) VALUES({ids[0]},@visit,'waiting_parts','虚构备件',DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 DAY));")
 before=sql('SELECT CONCAT(COUNT(*),\":\",SUM(version),\":\",(SELECT COUNT(*) FROM work_order_events),\":\",(SELECT COUNT(*) FROM work_order_visits),\":\",(SELECT COUNT(*) FROM work_order_visit_context)) FROM work_orders')
 # The marker is part of the literal location, so every assertion has a fixed scope.
 def q(text):return location+' '+text
 mode.write_text('insight-omit-location')
 conversation,data=turn('admin',q('最近出现最多的问题是什么'))
 mode.write_text('insight')
 check(data['business']['query']['location']==location,'explicit authorized location retained when model omits it')
 ambiguous=events(tokens['admin'],cid('admin'),location+' 多点验收区'+suffix+' 总结当前有哪些问题')
 check(ambiguous[-1][0]=='error' and ambiguous[-1][1]['code']=='analysis_scope_unavailable','multiple independent exact locations require a single scope')
 b=data['business'];check(b['kind']=='analysis' and b['query']['analysis']=='frequent','analysis rather than five-order search')
 check(b['total']==11 and len(b['items'])==5,'full recent aggregate differs from five examples')
 check(b['analysis']['categories'][0]=={'label':'照明','count':6} and b['analysis']['symptoms'][0]=={'label':'走廊灯不亮','count':6},'full category and exact-symptom ranking')
 check(b['analysis']['model_status']=='ready' and b['analysis']['notes'][0]['evidence'],'model synthesis grounded in fact ids')
 _,follow=turn('admin','那不限日期的高发问题呢',conversation);check(follow['business']['total']==12 and follow['business']['query']['location']==location,'analysis followup retains location and resets dates')
 _,no_show=turn('admin',q('现在有几单白跑了'));facts={f['id']:f for f in no_show['business']['analysis']['facts']}
 check(facts['no_show']['count']==2 and facts['no_show_orders']['count']==1 and no_show['business']['total']==1,'no-show event count and distinct orders use event dates')
 check(not no_show['business']['query'].get('keyword'),'explicit no-show is not description keyword')
 _,exception=turn('admin',q('有几单上门异常'));facts={f['id']:f for f in exception['business']['analysis']['facts']}
 check(facts['overdue']['count']==1 and facts['parts']['count']==1 and facts['exception_total']['count']==1,'old current exception included; overlapping queues deduplicated')
 check(exception['business']['total']==1 and exception['business']['items'][0]['id']==ids[0],'exception examples match exception cohort')
 for name,expected in [('requester',8),('requester2',4),('tech',2),('tech2',0),('other',1)]:
  _,answer=turn(name,q('不限日期 总结当前有哪些问题'))
  b=answer['business'];check(b['total']==expected and all(item['id']!=other['id'] for item in b['items']) if name!='other' else b['total']==expected and b['items'][0]['id']==other['id'],name+' organization/ownership/current-assignee isolation')
 _,zero=turn('admin','没有此楼 总结当前有哪些问题');check(zero['business']['total']==0 and zero['business']['analysis']['model_status']=='unavailable','zero data produces no fabricated model insight')
 for failure in ('insight-invalid','insight-number','insight-write','insight-timeout'):
  mode.write_text(failure);c,data=turn('admin',q('总结当前有哪些问题'));check(data['business']['analysis']['model_status']=='unavailable' and not data['business']['analysis']['notes'] and data['business']['total']==11,'facts-only fallback for '+failure)
 mode.write_text('insight');_,retry=turn('admin',q('总结当前有哪些问题'),c);check(retry['business']['analysis']['model_status']=='ready','explicit retry succeeds after suggestion failure')
 # Tool binding is still authoritative; restore it using the current version.
 config_before=request('GET','/api/agents?manage=1',tokens['admin'])['items'];a=next(x for x in config_before if x['id']==agents['admin'])
 try:
  request('PUT',f"/api/agents/{a['id']}",tokens['admin'],{**a,'tool_names':[]});ev=events(tokens['admin'],cid('admin'),q('总结当前有哪些问题'));check(ev[-1][0]=='error' and ev[-1][1]['code']=='tool_unavailable','disabled binding is not bypassed by analysis')
 finally:
  current=next(x for x in request('GET','/api/agents?manage=1',tokens['admin'])['items'] if x['id']==a['id']);request('PUT',f"/api/agents/{a['id']}",tokens['admin'],{**current,'tool_names':a['tool_names']})
 mode.write_text('insight-slow');conversation=cid('admin')
 with concurrent.futures.ThreadPoolExecutor() as pool:
  f=pool.submit(events,tokens['admin'],conversation,q('总结当前有哪些问题'));time.sleep(.2)
  try:events(tokens['admin'],conversation,q('总结当前有哪些问题'));raise AssertionError('parallel turn allowed')
  except urllib.error.HTTPError as e:check(e.code==409,'concurrent turn rejected by lock')
  check(f.result()[-1][0]=='done','first concurrent analysis completes')
 mode.write_text('insight-slow');cancel_c=cid('admin')
 req=urllib.request.Request(BASE+f'/api/conversations/{cancel_c}/messages',data=json.dumps({'content':q('总结当前有哪些问题')}).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+tokens['admin']})
 with urllib.request.urlopen(req,timeout=10) as response:
  for line in response:
   if '正在分析' in line.decode():break
 time.sleep(.4)
 for _ in range(20):
  state=request('GET',f'/api/conversations/{cancel_c}/messages',tokens['admin'])['items']
  if state and state[-1]['status']!='generating':break
  time.sleep(.1)
 check(state[-1]['status']=='cancelled' and not state[-1].get('business'),'cancelled analysis publishes no unconfirmed result')
 mode.write_text('insight');_,after_cancel=turn('admin',q('总结当前有哪些问题'),cancel_c);check(after_cancel['business']['analysis']['model_status']=='ready','cancel releases lock for explicit retry')
 mode.write_text('insight-slow');version_c=cid('admin')
 with concurrent.futures.ThreadPoolExecutor() as pool:
  future=pool.submit(events,tokens['admin'],version_c,q('总结当前有哪些问题'));time.sleep(.35)
  a=next(x for x in request('GET','/api/agents?manage=1',tokens['admin'])['items'] if x['id']==agents['admin'])
  request('PUT',f"/api/agents/{a['id']}",tokens['admin'],{**a,'name':a['name']+'临时版本验证'})
  try:
   ev=future.result();check(ev[-1][0]=='error' and ev[-1][1]['code']=='configuration_changed','configuration change prevents unconfirmed analysis publication')
  finally:
   latest=next(x for x in request('GET','/api/agents?manage=1',tokens['admin'])['items'] if x['id']==a['id']);request('PUT',f"/api/agents/{a['id']}",tokens['admin'],{**latest,'name':a['name']})
 mode.write_text('insight')
 subprocess.run(COMPOSE+['stop','-t','1','mcp'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  ev=events(tokens['admin'],cid('admin'),q('总结当前有哪些问题'));check(ev[-1][0]=='error' and ev[-1][1]['code']=='tool_unavailable','MCP failure is an error rather than zero data')
 finally:subprocess.run(COMPOSE+['start','mcp'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 check(before==sql('SELECT CONCAT(COUNT(*),\":\",SUM(version),\":\",(SELECT COUNT(*) FROM work_order_events),\":\",(SELECT COUNT(*) FROM work_order_visits),\":\",(SELECT COUNT(*) FROM work_order_visit_context)) FROM work_orders'),'all analysis paths leave business unchanged')
 history=request('GET',f'/api/conversations/{conversation}/messages',tokens['admin'])['items'];check(any(m.get('business',{}).get('analysis',{}).get('model_status')=='ready' for m in history),'analysis history preserves structured result')
 (FOLDER/'checks.json').write_text(json.dumps({'gateway':'mock','checks':checks},ensure_ascii=False,indent=2))
 print(len(checks),'analysis integration checks passed')
if __name__=='__main__':run()
