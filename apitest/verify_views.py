"""Verify role queues, full-scope summary and readable audit without printing credentials."""
from pathlib import Path
import json,urllib.request,urllib.error,uuid,time
root=Path(__file__).resolve().parents[1]
env=dict(l.split('=',1) for l in (root/'tmp/check.env').read_text().splitlines() if '=' in l and not l.startswith('#'))
base='http://127.0.0.1:'+env['WEB_PORT']
checks=0

def call(method,path,data=None,token=None,extra=None):
 hs={'Content-Type':'application/json',**(extra or {})}
 if token:hs['Authorization']='Bearer '+token
 req=urllib.request.Request(base+path,data=json.dumps(data).encode() if data is not None else None,headers=hs,method=method)
 try:
  with urllib.request.urlopen(req,timeout=20) as r:return r.status,json.load(r)
 except urllib.error.HTTPError as e:return e.code,json.load(e)

def expect(result,status=200):
 global checks
 assert result[0]==status,f'Unexpected HTTP status: {result[0]} expected {status}'
 checks+=1;return result[1]

def login(name,key):return expect(call('POST','/api/login',{'username':name,'password':env[key]}))['access_token']
for attempt in range(60):
 try:
  if call('GET','/health/ready')[0]==200:break
 except Exception:pass
 time.sleep(.5)
else:raise RuntimeError('API not ready')
r=login('requester','SEED_REQUESTER_PASSWORD');a=login('admin','SEED_ADMIN_PASSWORD');t=login('tech','SEED_TECH_PASSWORD');other=login('other','SEED_OTHER_PASSWORD');oa=login('other_admin','SEED_OTHER_PASSWORD')
order=expect(call('POST','/api/work-orders',{'category':'lighting','location':'概览验收位置','description':'需补充说明'},r,{'Idempotency-Key':str(uuid.uuid4())}),201)['order']
order=expect(call('POST',f"/api/work-orders/{order['id']}/review",{'version':order['version'],'decision':'request_info','reason':'请写明楼层'},a))['order']
for i in range(21):expect(call('POST','/api/work-orders',{'category':'lighting','location':f'分页验收-{i}','description':'待审核灯具'},r,{'Idempotency-Key':str(uuid.uuid4())}),201)
roles={'requester':(r,{'needs_info','awaiting_confirmation'}),'admin':(a,{'submitted','unassigned'}),'technician':(t,{'assigned','in_progress'})}
terminal={'closed','rejected','cancelled'}
for role,(token,states) in roles.items():
 summary=expect(call('GET','/api/work-orders/summary',token=token));first=expect(call('GET','/api/work-orders?page_size=20',token=token));assert summary['total']==first['total']
 allrows=[]
 for page in range(1,(first['total']+99)//100+1):allrows+=expect(call('GET',f'/api/work-orders?page_size=100&page={page}',token=token))['items']
 assert summary['total']==len(allrows) and summary['action']==sum(o['status'] in states for o in allrows)
 assert summary['finished']==sum(o['status'] in terminal for o in allrows) and summary['active']+summary['finished']==summary['total']
 for queue in ['action','active','finished']:
  rows=expect(call('GET','/api/work-orders?queue='+queue+'&page_size=100',token=token));assert rows['total']==summary[queue]
  if queue=='action':assert all(o['status'] in states for o in rows['items'])
  if queue=='finished':assert all(o['status'] in terminal for o in rows['items'])
 expect(call('GET','/api/work-orders?queue=invalid',token=token),400)
summary=expect(call('GET','/api/work-orders/summary',token=r));assert summary['total']>20 and summary['action']>=1
waiting=expect(call('GET','/api/work-orders?queue=action&page_size=100',token=r));assert order['id'] in [o['id'] for o in waiting['items']]
for token in [other,oa]:
 rows=expect(call('GET','/api/work-orders?queue=all&page_size=100',token=token));summary=expect(call('GET','/api/work-orders/summary',token=token));assert rows['total']==summary['total'];assert order['id'] not in [o['id'] for o in rows['items']]
audit=expect(call('GET',f"/api/audit?resource_id={order['id']}",token=a));assert any(v['actor_name']=='后勤管理员' and v['order_number']==order['number'] for v in audit['items'])
assert all(v['actor_name'] in ('后勤管理员','报修人一') for v in audit['items'])
expect(call('GET','/api/work-orders/summary'),401)
print(f'PASS: {checks} view API checks; role queues, >20-row summary, isolation and audit labels.')
