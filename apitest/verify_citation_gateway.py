"""Issue #4: three real-gateway questions in a fresh isolated project (paid calls).
Only fictional data; stops at the first failure, retains volumes and local evidence.
"""
import json,time,secrets
from pathlib import Path
from verify_gateway import Verification
v=Verification(18089)
try:
 v.dc('up','-d','--no-build')
 for _ in range(120):
  try:v.request('GET','/health/ready');break
  except (RuntimeError,OSError):time.sleep(.5)
 tokens={}
 for name,key in [('admin','SEED_ADMIN_PASSWORD'),('requester','SEED_REQUESTER_PASSWORD'),('requester2','SEED_REQUESTER2_PASSWORD')]:
  tokens[name]=v.request('POST','/api/login',body={'username':name,'password':v.passwords[key]})['access_token']
 admin=tokens['admin'];user=tokens['requester']
 text='仅供虚构软件验收，不是校方制度。报修应提供具体楼栋、房间或公共区域位置、照明/电器/给排水类别和故障现象。发现灯具冒烟、设备异味或漏水接近电器时，停止使用并联系现场工作人员，在确保自身安全的前提下远离故障区域；不要自行拆卸、接线或带电维修。本材料没有规定维修完成时限。'
 boundary='issue4'+secrets.token_hex(8)
 body=(f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n演示报修信息与电路安全\r\n--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="demo.md"\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
 mid=v.request('POST','/api/materials',admin,body,201,{'Content-Type':'multipart/form-data; boundary='+boundary})['id']
 for _ in range(120):
  material=v.request('GET',f'/api/materials/{mid}',admin)['material']
  if material['status'] in ('ready','failed'):break
  time.sleep(.5)
 v.check(material['status']=='ready' and material['vector_ready'],'真实嵌入发布')
 agent=next(a for a in v.request('GET','/api/agents?manage=1',admin)['items'] if a['role']=='requester')
 agent.update(enabled=True,material_ids=[mid])
 v.request('PUT',f'/api/agents/{agent["id"]}',admin,agent)
 answers=[];v.evidence['answers']=answers
 for question in ('报修需要填写哪些信息？','维修时有哪些安全注意事项？','不要自行拆电路或进行带电操作'):
  cid,meta,done=v.turn(agent['id'],user,question)
  answers.append({'question':question,**done})
  v.check(bool(done['citations']),'实际引用存在')
  for ref in done['citations']:
   path=f'/api/conversations/{cid}/citations/{done["message_id"]}/{ref["chunk_id"]}'
   source=v.request('GET',path,user)
   v.check(source['text'][ref['start']:ref['end']]==ref['text'],'原文范围一致')
   v.request('GET',path,tokens['requester2'],status=404)
  v.check(True,'他人无法读取引用')
 v.evidence['result']='passed'
except Exception as e:
 v.evidence['result']='failed';print('FAIL '+(str(e) if isinstance(e,RuntimeError) else type(e).__name__),flush=True)
finally:
 v.dc('down')
 Path('tmp/issue4-gateway-result.json').write_text(json.dumps(v.evidence,ensure_ascii=False,indent=2))
 print('Saved tmp/issue4-gateway-result.json; isolated containers stopped, volumes retained.',flush=True)
if v.evidence['result'] != 'passed':raise SystemExit(1)
