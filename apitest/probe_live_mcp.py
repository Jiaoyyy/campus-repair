"""Read-only deployment smoke: current admin binding, real MCP, no model or fixture writes.
Run from the repository root. Secrets are passed to the temporary client through stdin.
"""
import json, subprocess
from pathlib import Path

CLIENT = r'''
import base64, hashlib, hmac, json, sys, time, urllib.request, uuid
cfg=json.load(sys.stdin)
def request(url,body,headers):
 req=urllib.request.Request(url,data=json.dumps(body).encode() if body is not None else None,headers=headers)
 with urllib.request.urlopen(req,timeout=15) as r:return json.load(r) if r.status!=202 else {},dict(r.headers)
api='http://api:8080'
login,_=request(api+'/api/login',{'username':'admin','password':cfg['SEED_ADMIN_PASSWORD']},{'Content-Type':'application/json'})
token=login['access_token'];auth={'Content-Type':'application/json','Authorization':'Bearer '+token}
agents,_=request(api+'/api/agents?manage=1',None,auth)
a=next(x for x in agents['items'] if x['role']=='admin' and x['enabled'])
assert 'query_work_orders' in a['tool_names'],'query tool not bound'
now=int(time.time());rid=uuid.uuid4().hex
claims={'session_id':hmac.new(cfg['SESSION_SECRET'].encode(),token.encode(),hashlib.sha256).hexdigest(),'agent_id':a['id'],'request_id':rid,'audience':'campus-repair-mcp','iat':now,'exp':now+60}
payload=base64.urlsafe_b64encode(json.dumps(claims,separators=(',',':')).encode()).decode().rstrip('=')
sig=base64.urlsafe_b64encode(hmac.new(cfg['MCP_CALL_SECRET'].encode(),payload.encode(),hashlib.sha256).digest()).decode().rstrip('=')
credential=payload+'.'+sig
headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream','Authorization':'Bearer '+credential}
def rpc(method,params,idx=1):return request('http://mcp:8081/mcp',{'jsonrpc':'2.0','id':idx,'method':method,'params':params},headers)
v,h=rpc('initialize',{'protocolVersion':'2025-11-25','capabilities':{},'clientInfo':{'name':'readonly-deployment-probe','version':'1'}})
headers['Mcp-Protocol-Version']=v['result']['protocolVersion']
if h.get('Mcp-Session-Id'):headers['Mcp-Session-Id']=h['Mcp-Session-Id']
q={'queue':'all','location':'研发楼','period':'recent'}
v,_=rpc('tools/call',{'name':'query_work_orders','arguments':q,'_meta':{'campus_repair_auth':credential}},2)
r=v.get('result',{});assert not r.get('isError') and 'structuredContent' in r,'MCP query failed'
b=r['structuredContent'];assert b['query']==q and b['kind']=='orders'
assert b['total']==sum(b['statuses'].values())
print(json.dumps({'result':'passed','request_id':rid,'query':q,'total':b['total'],'time_basis':b['time_basis']},ensure_ascii=False))
request(api+'/api/logout',{},auth)
'''
if __name__=='__main__':
 cfg=json.loads(subprocess.check_output(['docker','compose','config','--format','json']))
 env=cfg['services']['api']['environment']
 data={k:env[k] for k in ['SEED_ADMIN_PASSWORD','SESSION_SECRET','MCP_CALL_SECRET']}
 network=cfg['name']+'_default'
 r=subprocess.run(['docker','run','--rm','-i','--network',network,'python:3.12-alpine','python','-c',CLIENT],input=json.dumps(data),capture_output=True,text=True)
 if r.returncode:raise SystemExit('Live MCP smoke failed; raw error suppressed')
 result=json.loads(r.stdout)
 path=Path('tmp/live-mcp-deployment-probe.json');path.parent.mkdir(exist_ok=True);path.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
 print(r.stdout.strip())
