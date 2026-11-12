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
def mcp_requests():return {x['request_id'] for x in req('GET','/api/audit?page_size=100',admin)['items'] if x['action']=='mcp.call'}
original=list(agent['tool_names'])
mode_path=root/'tmp/draft-mock/chat-mode'
original_mode=mode_path.read_bytes() if mode_path.exists() else None
request_log=root/'tmp/draft-mock/chat-requests.jsonl'
def request_count():return len(request_log.read_text().splitlines()) if request_log.exists() else 0
try:
    configure(['query_work_orders','list_dispatch_candidates'])
    req('POST','/api/work-orders',requester,{'category':'lighting','location':'研发楼语义故障测试','description':'虚构验收灯不亮'})
    before=req('GET','/api/work-orders/summary',admin)
    mode('plan-location');cid=conversation();events=turn(cid,'研发楼最近有什么故障');done=events[-1][1]
    check(events[-1][0]=='done' and done['business']['query']['location']=='研发楼','合法原生计划确实调用查询')
    snapshot=done['business']
    for value in ['plan-identity','plan-write','tools','malformed','fail','rate-limit','quota-limit','plan-disconnect','plan-timeout']:
        mode(value);count_before=request_count();events=turn(cid,'研发楼最近有什么故障');check(events[-1][0]=='error' and 'business' not in events[-1][1],value+' 不发布查询结果')
        check(events[0][1]['request_id'] not in mcp_requests(),value+' 未产生MCP调用')
        if value in ('rate-limit','quota-limit'):check(events[-1][1]['code']==('query_rate_limited' if value=='rate-limit' else 'query_quota_exhausted'),'429 与额度不足提示准确')
        if value in ('plan-disconnect','plan-timeout'):
            check(events[-1][1]['code']=='query_gateway_unavailable' and ('超时' if value=='plan-timeout' else '无法连接') in events[-1][1]['message'],value+' 正确区分连接与超时')
            check(request_count()==count_before+1,value+' 仅请求一次不自动重试')
            check(not any(k=='delta' for k,_ in events),value+' 不发布未核验正文')
        elif value in ('plan-identity','plan-write','tools','malformed'):
            check(events[-1][1]['code']=='query_plan_failed',value+' 仍按非法计划拒绝')
        history=req('GET',f'/api/conversations/{cid}/messages',admin)['items']
        check(history[-1]['status']=='failed' and not history[-1].get('business'),value+' 失败历史不保存新快照')
    mode('false-business');events=turn(conversation(),'聊聊研发楼')
    check(events[-1][0]=='done' and '尚未取得' in events[-1][1]['answer'],'没有工具证据时拦截假装查无记录')
    check(all('目前没有查询到' not in v.get('text','') for k,v in events if k=='delta'),'假查询文字不经delta泄露')
    configure([]);mode('plan-location');events=turn(conversation(),'研发楼最近有什么故障')
    check(events[-1][0]=='error' and events[-1][1]['code']=='tool_unavailable','解绑工具不能绕过MCP查询')
    check(before==req('GET','/api/work-orders/summary',admin),'所有计划失败和只读调用不改变业务计数')
finally:
    configure(original)
    if original_mode is None:mode_path.unlink(missing_ok=True)
    else:mode_path.write_bytes(original_mode)
(root/'tmp/semantic-failures.json').write_text(json.dumps({'result':'passed','checks':checks},ensure_ascii=False,indent=2)+'\n')
print(f'通过 {len(checks)} 项语义失败检查')
