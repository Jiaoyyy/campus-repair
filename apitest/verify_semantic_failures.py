"""Deterministic native plan failures and no-evidence guard, isolated only."""
import json, subprocess, urllib.request, urllib.error, uuid
from pathlib import Path
root=Path(__file__).resolve().parents[1]
env=dict(l.split('=',1) for l in (root/'tmp/draft-check.env').read_text().splitlines() if '=' in l and not l.startswith('#'))
base='http://127.0.0.1:'+env['WEB_PORT']
compose=['docker','compose','-p','campus-repair-draft-check','--env-file','tmp/draft-check.env','-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml','-f','compose.draft-check.yaml']
checks=[]
def check(v,label):
    if not v:raise AssertionError(label)
    checks.append(label);print('PASS '+label,flush=True)
def mode(value):
    subprocess.run(compose+['exec','-T','chat-mock','python','-c',"from pathlib import Path;Path('/state/chat-mode').write_text("+repr(value)+")"],check=True,capture_output=True)
def req(method,path,token=None,body=None):
    hs={'Content-Type':'application/json','Idempotency-Key':uuid.uuid4().hex}
    if token:hs['Authorization']='Bearer '+token
    with urllib.request.urlopen(urllib.request.Request(base+path,method=method,headers=hs,data=json.dumps(body).encode() if body is not None else None),timeout=20) as r:return json.load(r)
def turn(cid,text):
    events=[];kind='';parts=[]
    with urllib.request.urlopen(urllib.request.Request(base+f'/api/conversations/{cid}/messages',headers={'Content-Type':'application/json','Authorization':'Bearer '+admin},data=json.dumps({'content':text}).encode()),timeout=20) as r:
        for raw in r:
            line=raw.decode().strip()
            if line.startswith('event:'):kind=line[6:].strip()
            elif line.startswith('data:'):parts.append(line[5:].strip())
            elif not line and kind:events.append((kind,json.loads(''.join(parts))));kind='';parts=[]
    check(events[0][0]=='meta' and sum(k in ('done','error') for k,_ in events)==1,'唯一终止事件')
    return events
admin=req('POST','/api/login',body={'username':'admin','password':env['SEED_ADMIN_PASSWORD']})['access_token']
requester=req('POST','/api/login',body={'username':'requester','password':env['SEED_REQUESTER_PASSWORD']})['access_token']
agent=next(x for x in req('GET','/api/agents?manage=1',admin)['items'] if x['role']=='admin')
def configure(tools):
    global agent
    agent=req('PUT',f"/api/agents/{agent['id']}",admin,{**agent,'tool_names':tools})['agent']
def conversation():return req('POST','/api/conversations',admin,{'agent_id':agent['id']})['conversation']['id']
def successes():return {x['request_id'] for x in req('GET','/api/audit?page_size=100',admin)['items'] if x['action']=='mcp.call' and x['outcome']=='success'}
original=list(agent['tool_names'])
try:
    configure(['query_work_orders','list_dispatch_candidates'])
    req('POST','/api/work-orders',requester,{'category':'lighting','location':'研发楼语义故障测试','description':'虚构验收灯不亮'})
    before=req('GET','/api/work-orders/summary',admin)
    mode('plan-location');cid=conversation();events=turn(cid,'研发楼最近有什么故障');done=events[-1][1]
    check(events[-1][0]=='done' and done['business']['query']['location']=='研发楼','合法原生计划确实调用查询')
    snapshot=done['business']
    for value in ['plan-identity','plan-write','tools','malformed','fail','rate-limit','quota-limit']:
        mode(value);events=turn(cid,'研发楼最近有什么故障');check(events[-1][0]=='error' and 'business' not in events[-1][1],value+' 不发布查询结果')
        check(events[0][1]['request_id'] not in successes(),value+' 未产生成功MCP调用')
        if value in ('rate-limit','quota-limit'):check(events[-1][1]['code']==('query_rate_limited' if value=='rate-limit' else 'query_quota_exhausted'),'429 与额度不足提示准确')
        history=req('GET',f'/api/conversations/{cid}/messages',admin)['items']
        check(history[-1]['status']=='failed' and not history[-1].get('business'),value+' 失败历史不保存新快照')
    mode('false-business');events=turn(conversation(),'聊聊研发楼')
    check(events[-1][0]=='done' and '尚未取得' in events[-1][1]['answer'],'没有工具证据时拦截假装查无记录')
    check(all('目前没有查询到' not in v.get('text','') for k,v in events if k=='delta'),'假查询文字不经delta泄露')
    configure([]);mode('plan-location');events=turn(conversation(),'研发楼最近有什么故障')
    check(events[-1][0]=='error' and events[-1][1]['code']=='tool_unavailable','解绑工具不能绕过MCP查询')
    check(before==req('GET','/api/work-orders/summary',admin),'所有计划失败和只读调用不改变业务计数')
finally:
    configure(original);mode('ok')
(root/'tmp/semantic-failures.json').write_text(json.dumps({'result':'passed','checks':checks},ensure_ascii=False,indent=2)+'\n')
print(f'通过 {len(checks)} 项语义失败检查')
