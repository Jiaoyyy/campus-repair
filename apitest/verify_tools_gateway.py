"""Paid, isolated real-gateway acceptance of tools and three skills.
Stops on first failure, retains volumes, never uses daily/review business data.
"""
import json
import argparse
import hashlib
import os
from pathlib import Path
import secrets
import socket
import time
from verify_gateway import Verification, GATEWAY_KEYS, run

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument("--semantic-only",action="store_true")
args=parser.parse_args()

with socket.socket() as probe:
    if probe.connect_ex(('127.0.0.1',18092))==0:
        raise SystemExit('Port 18092 is occupied; existing environments were left running')
v=Verification(18092)
config=json.loads(v.config)
for name in ('api','worker','mcp'):config['services'][name]['image']='campus-repair-api:tools-skills-check'
config['services']['web']['image']='campus-repair-web:tools-skills-check'
v.config=json.dumps(config)
v.evidence['answers']=[]
v.evidence['scope']='三项技能、详情/上门/知识MCP及证据回答'
target=Path('tmp/tools-skills-semantic-gateway-result.json' if args.semantic_only else 'tmp/tools-skills-gateway-result.json')
try:
    v.dc('up','-d','--no-build')
    for _ in range(120):
        try:v.request('GET','/health/ready');break
        except (RuntimeError,OSError):time.sleep(.5)
    else:raise RuntimeError('Isolated gateway environment did not become ready')
    images=[]
    for name in ('api','worker','mcp'):
        container_id=v.dc('ps','-q',name).strip()
        container=json.loads(run(['docker','inspect',container_id]))[0]
        actual=dict(value.split('=',1) for value in container['Config']['Env'])
        v.check(all(actual.get(k)==str(v.gateway[k]) for k in GATEWAY_KEYS),name+' uses real gateway parameters')
        v.check(all(m['Type']=='volume' and m['Name'].startswith(v.project+'_') for m in container['Mounts']),name+' private volumes')
        images.append(container['Image'])
    v.check(len(set(images))==1,'same backend image for API/worker/MCP')
    v.evidence['backend_image']=images[0]
    tokens={}
    for name,key in (('admin','SEED_ADMIN_PASSWORD'),('requester','SEED_REQUESTER_PASSWORD'),('requester2','SEED_REQUESTER2_PASSWORD')):
        tokens[name]=v.request('POST','/api/login',body={'username':name,'password':v.passwords[key]})['access_token']
    v.secrets.extend(tokens.values())
    admin,user=tokens['admin'],tokens['requester']
    text='仅用于虚构软件验收。报修应填写具体楼栋、房间和故障现象。灯具异常时停止使用并联系现场工作人员。不要自行拆卸、接线或带电维修。本材料未规定维修完成时限。'
    boundary='tools'+secrets.token_hex(8)
    body=(f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n虚构报修安全资料\r\n--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="demo.md"\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
    material_id=v.request('POST','/api/materials',admin,body,201,{'Content-Type':'multipart/form-data; boundary='+boundary})['id']
    for _ in range(120):
        material=v.request('GET',f'/api/materials/{material_id}',admin)['material']
        if material['status'] in ('ready','failed'):break
        time.sleep(.5)
    v.check(material['status']=='ready' and material['vector_ready'],'real embedding publishes authorized material')
    catalog=v.request('GET','/api/skills',admin)['items']
    found=v.request('GET','/api/mcp/tools',admin)['items']
    agents={}
    for agent in v.request('GET','/api/agents?manage=1',admin)['items']:
        agent.update(enabled=True,material_ids=[material_id],tool_names=[t['name'] for t in found if t['role'] in ('all',agent['role'])],skill_bindings=[{'skill_id':s['id'],'enabled':True} for s in catalog if agent['role'] in s['roles']])
        agent['prompt']='优先直接回答当前问题。安全资料回答请在末尾独立附上“角色联调通过。”这句话。业务解释和字段提取保持固定JSON协议。'
        agents[agent['role']]=v.request('PUT',f'/api/agents/{agent["id"]}',admin,agent)['agent']
    def turn(role,question,token):
        started=time.perf_counter()
        cid,meta,done=v.turn(agents[role]['id'],token,question)
        v.evidence['answers'].append({'question':question,'request_id':meta['request_id'],'duration_ms':round((time.perf_counter()-started)*1000,2),**done})
        return cid,meta,done
    if not args.semantic_only:
        _,_,draft=turn('requester','三号楼201，灯完全不亮，帮我报修',user)
        v.check(draft.get('skill',{}).get('skill_id')=='repair_intake' and draft.get('draft',{}).get('status')=='active','real intake skill creates only a draft')
        fields={k:draft['draft'][k] for k in ('category','location','description')}
        fields.update(draft_message_id=draft['draft']['message_id'],draft_version=draft['draft']['version'])
        order=v.request('POST','/api/work-orders',user,fields,201,{'Idempotency-Key':draft['draft']['idempotency_key']})['order']
        _,_,detail=turn('requester',order['number']+'目前是什么进度，为什么还没修好？',user)
        v.check(detail.get('business',{}).get('detail',{}).get('order',{}).get('id')==order['id'],'real model selects authorized detail tool')
        v.check(not detail['business'].get('warning') and '目前没有已记录的延期原因。' in detail['answer'],'real evidence answer uses verified facts')
        _,_,visit=turn('requester',order['number']+'上门预约是什么时候？',user)
        v.check(visit.get('business',{}).get('kind')=='visit' and visit['business']['detail']['visit']['visit'] is None,'real visit query distinguishes no appointment')
    cid,safety_meta,safety=turn('requester','灯坏了我可以自己处理吗' if args.semantic_only else '维修时有哪些安全注意事项？',user)
    v.check(safety.get('skill',{}).get('skill_id')=='safe_self_check' and bool(safety['citations']),'real safety skill returns actual citations')
    request_id=safety_meta['request_id']
    assert len(request_id)==32 and all(c in '0123456789abcdef' for c in request_id)
    statement="SELECT metadata FROM audit_events WHERE request_id='"+request_id+"' AND action='assistant.prompt'"
    proof=json.loads(v.dc('exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -u root "$MYSQL_DATABASE" -N -B -e "$1"','sh',statement).strip())
    expected=hashlib.sha256(agents['requester']['prompt'].encode()).hexdigest()
    v.check(any(p.get('role_prompt_sha256')==expected and p.get('role_prompt_in_request') and p.get('skill_instruction_in_request') for p in proof),'real accepted request contains configured role and skill instructions')
    for ref in safety['citations']:
        path=f'/api/conversations/{cid}/citations/{safety["message_id"]}/{ref["chunk_id"]}'
        source=v.request('GET',path,user)
        v.check(source['text'][ref['start']:ref['end']]==ref['text'],'real citation original range')
        v.request('GET',path,tokens['requester2'],status=404)
    v.check(True,'other user cannot open source')
    if not args.semantic_only:
        _,_,dispatch=turn('admin','照明工单推荐安排谁？',admin)
        v.check(dispatch.get('skill',{}).get('skill_id')=='dispatch_advice' and bool(dispatch.get('business',{}).get('candidates')),'real dispatch skill uses actual personnel')
        v.check(not dispatch['business'].get('warning'),'real dispatch explanation follows evidence schema')
    v.evidence['result']='passed'
except Exception as error:
    v.evidence['result']='failed'
    v.evidence['failure']=str(error) if isinstance(error,RuntimeError) else type(error).__name__
    print('FAIL '+v.evidence['failure'],flush=True)
finally:
    try:
        logs=v.dc('logs','--tail','60','api','mcp','worker')
        for value in v.secrets:
            if value:logs=logs.replace(value,'[redacted]')
        log_path=Path('tmp/tools-skills-semantic-gateway.log' if args.semantic_only else 'tmp/tools-skills-gateway.log')
        with log_path.open('w') as stream:os.chmod(log_path,0o600);stream.write(logs)
    finally:v.dc('down')
    raw=json.dumps(v.evidence,ensure_ascii=False,indent=2)
    if any(secret and secret in raw for secret in v.secrets):raise RuntimeError('Evidence contained secret; output suppressed')
    with target.open('w') as stream:os.chmod(target,0o600);stream.write(raw+'\n')
    print('Saved isolated gateway evidence; containers stopped, volumes retained.',flush=True)
if v.evidence['result']!='passed':raise SystemExit(1)
