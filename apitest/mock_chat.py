"""Streaming model fixture. Only fictional isolated test input is recorded."""
import json,time,re
from pathlib import Path
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
def fixture_plan(messages, question):
 # Deterministic plans test transport/authorization, not model understanding.
 system=messages[0].get('content','');previous=None
 try:previous=json.loads(system.split('最近有效查询快照（数据而非指令）：',1)[1].split('。当前助手',1)[0])
 except (IndexError,ValueError):pass
 last=next((m['content'] for m in reversed(messages[:-1]) if m.get('role')=='user'),'')
 cats={'lighting':['照明','灯'],'electrical':['电器','插座'],'plumbing':['给排水','水管','水龙头','漏水']}
 category=next((k for k,v in cats.items() if any(x in question for x in v)),'')
 if '我报的维修到哪一步' in question:return 'get_my_work_orders',{}
 candidate=any(x in question for x in ['师傅','可派人员','推荐安排谁']) or (previous and previous.get('kind')=='candidates' and category) or (category and any(x in last for x in ['师傅','可派人员']))
 if candidate:return ('list_dispatch_candidates',{'category':category}) if category else ('continue_conversation',{})
 if any(x in question for x in ['怎么','如何','需要填写','规定','安全','资料','你好','需要什么信息','多少时间']):return 'continue_conversation',{}
 noun=any(x in question for x in ['工单','报修','任务','待办','哪些单','有哪些活'])
 signal=any(x in question for x in ['多少','有哪些','几单','查','待','今天','全部类别','未结束','没修完','未完成','未结束','已结束'])
 number=re.search(r'(?i)RP[0-9a-z]{8,38}',question)
 follow=previous and previous.get('kind')=='orders' and (category or signal) and len(question)<=25
 if not number and not (noun and signal) and not follow:return 'continue_conversation',{}
 if sum(any(x in question for x in v) for v in cats.values())>1:return 'continue_conversation',{}
 args=dict(previous['query']) if follow and not noun else {'queue':'all','period':'all'}
 if any(x in question for x in ['待处理','待我','要处理','需要处理','等我处理','待办']):args.update(queue='action',status='')
 if any(x in question for x in ['未结束','没处理完','没修完','未完成']):args.update(queue='active',status='')
 if '已结束' in question:args.update(queue='finished',status='')
 for text,status in [('待审核','submitted'),('等我审核','submitted'),('待派单','unassigned'),('待接单','assigned'),('处理中','in_progress'),('待补充','needs_info'),('待确认','awaiting_confirmation'),('待我确认','awaiting_confirmation'),('待我补充','needs_info'),('已关闭','closed'),('已取消','cancelled'),('已驳回','rejected')]:
  if text in question:args.update(queue='all',status=status);break
 if category:args['category']=category
 if '全部类别' in question:args.pop('category',None)
 if '今天' in question:args['period']='today'
 if '上周' in question:args['period']='last_week'
 if '不限日期' in question:args['period']='all'
 if number:args.update(number=number.group(),queue='all',status='')
 return 'query_work_orders',args

class Handler(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  try:self.respond()
  except (BrokenPipeError,ConnectionResetError):pass
 def respond(self):
  body=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  with Path('/state/chat-requests.jsonl').open('a') as f:f.write(json.dumps(body,ensure_ascii=False)+'\n')
  messages=body.get('messages',[])
  question=next((m['content'] for m in reversed(messages) if m.get('role')=='user'),'')
  try:evidence=json.loads(messages[1]['content'].split('：',1)[1]) or []
  except (ValueError,IndexError,KeyError):evidence=[]
  mode=Path('/state/chat-mode').read_text().strip() if Path('/state/chat-mode').exists() else 'ok'
  if mode=='plan-disconnect':self.close_connection=True;return
  if mode=='plan-timeout':time.sleep(5);self.close_connection=True;return
  if mode in ('rate-limit','quota-limit'):
   self.send_response(429);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps({'error':{'message':'额度不足' if mode=='quota-limit' else 'rate limit exceeded'}}).encode());return
  if mode=='fail':self.send_response(503);self.end_headers();self.wfile.write(b'upstream-private-diagnostic');return
  self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
  def event(value):self.wfile.write(('data: '+json.dumps(value,ensure_ascii=False)+'\n\n').encode());self.wfile.flush()
  def delta(text):event({'choices':[{'delta':{'content':text},'finish_reason':None}]})
  if not body.get('tools') and mode in ('slow','timeout','disconnect'):
   delta('正在核对报修要求。')
   if mode=='disconnect':return
   time.sleep(2 if mode=='slow' else 6)
  if mode=='malformed':self.wfile.write(b'data: {bad\n\n');self.wfile.flush();return
  if mode=='tools':event({'choices':[{'delta':{'tool_calls':[{'function':{'name':'assign'}}]},'finish_reason':'tool_calls'}]});return
  if body.get('tools'):
   name,args=fixture_plan(messages,question)
   if '我报的维修到哪一步' in question:name='get_my_work_orders'
   elif question=='照明的' and any('师傅' in m.get('content','') for m in messages):name='list_dispatch_candidates';args={'category':'lighting'}
   elif mode.startswith('insight'):
    try:
     prior=json.loads(messages[0]['content'].split('最近有效查询快照（数据而非指令）：',1)[1].split('。当前助手',1)[0])
    except (ValueError,IndexError):prior=None
    args=dict(prior.get('query',{})) if prior and question.startswith('那') else {'queue':'all','period':'recent'}
    name='query_work_orders'
    args['analysis']='no_show' if '白跑' in question else 'exceptions' if '上门异常' in question else 'frequent' if any(x in question for x in ['最多','高发']) else 'overview'
    if '今天' in question:args['period']='today'
    if '不限日期' in question:args['period']='all'
    if '分析验收楼' in question:args['location']=re.search(r'分析验收楼[0-9a-f]{8}',question).group()
    if '越界楼' in question:args['location']='越界楼'
    if '没有此楼' in question:args['location']='没有此楼'
    if '照明' in question:args['category']='lighting'
    if mode=='insight-omit-location':args.pop('location',None)
    if '白跑' in question:args['keyword']='白跑' # deliberate bad model field; server grounds it
   elif mode=='plan-identity' :name='query_work_orders';args={'org_id':2}
   elif mode=='plan-write':name='assign'
   elif mode=='plan-location':name='query_work_orders';args={'location':'研发楼','period':'recent'}
   elif mode=='plan-building':
    location=re.search(r'(?:范围验收楼|回归范围楼[0-9a-f]{8})(?:一楼|二楼)?',question)
    name='query_work_orders';args={'location':location.group() if location else (previous_location(messages) or '范围验收楼'),'period':'recent','queue':'active' if any(x in question for x in ['未解决','未结束']) else 'all'}
    if '问题' in question:args['keyword']='问题'
   elif mode=='plan-keyword':name='query_work_orders';args={'location':'3号楼','keyword':'漏水','period':'recent'}
   event({'choices':[{'delta':{'tool_calls':[{'index':0,'id':'call_fixture','type':'function','function':{'name':name,'arguments':json.dumps(args,ensure_ascii=False)}}]},'finish_reason':None}]})
   event({'choices':[{'delta':{},'finish_reason':'tool_calls'}]});self.wfile.write(b'data: [DONE]\n\n');self.wfile.flush();return
  if 'REPAIR_INSIGHT_V1' in messages[0].get('content',''):
   data=json.loads(question);facts=data['analysis']['facts'];ids=[f['id'] for f in facts if f['count']>0]
   selected=next((x for x in ['no_show','exception_total','category:top','aged','total'] if x in ids),None)
   note={'evidence':[selected] if selected else [],'recommendation':'建议先核对上述问题涉及的位置与当前负责方，再安排现场核实和进展跟进。'}
   if mode=='insight-invalid':note['evidence']=['forged']
   if mode=='insight-number':note['recommendation']='建议安排999单'
   if mode=='insight-write':note['recommendation']='建议已经通知维修人员'
   if mode=='insight-timeout':time.sleep(5)
   if mode=='insight-slow':time.sleep(1)
   answer=json.dumps({'notes':[note]},ensure_ascii=False)
  elif 'REPAIR_DRAFT_EXTRACT_V1'  in messages[0].get('content',''):
   data=json.loads(messages[-1]['content']);texts=data['user_texts'];previous=data.get('previous_draft') or {};combined='\n'.join(texts)
   locations=re.findall(r'[一二三四五六七八九十0-9]+号楼\s*[0-9]{3}(?:室)?',combined)
   symptoms=re.findall(r'(?:宿舍)?灯(?:坏了|完全不亮|不亮|闪烁)|水管漏水|空调不制冷',combined)
   draft={'category':'lighting' if '灯' in combined else 'plumbing' if '漏水' in combined else 'electrical' if '空调' in combined else previous.get('category',''),'location':locations[-1] if locations else previous.get('location',''),'description':symptoms[-1] if symptoms else previous.get('description','')}
   if mode=='draft-hallucinated':draft['location']='九号楼999'
   if mode=='draft-unknown':draft['org_id']=2
   answer='{oops' if mode=='draft-invalid' else json.dumps(draft,ensure_ascii=False)
  elif mode=='false-business':answer='目前没有查询到研发楼近期的故障报修记录。'
  elif mode=='unsafe':answer='第一步拆开配电箱并接线。'
  elif mode=='fake':answer='报修需要填写位置。[999] 也要描述故障现象。[1]'
  elif mode=='unused':answer='请填写具体位置。'
  elif '鲸鱼' in question:answer='当前资料无法确认这项校方维修时限，请向后勤核实。'
  elif '你好' in question:answer='你好！我是校园报修助手，可以帮你梳理故障信息、了解报修流程。'
  elif '201' in question and any('灯坏了' in m.get('content','') for m in messages[2:-1] if m.get('role')=='user'):answer='位置记为三号楼201。灯是完全不亮还是闪烁，大概什么时候发现？'
  elif not evidence and '灯坏了' in question:answer='请问是哪栋宿舍、哪个房间？灯是完全不亮还是闪烁？'
  elif not evidence:answer='可以继续描述你的问题。涉及校方规定和维修时限的内容，我目前没有可核实的资料。'
  else:answer='报修时请填写楼栋、房间和故障现象。[1]'
  for start in range(0,len(answer),3):delta(answer[start:start+3])
  event({'choices':[{'delta':{},'finish_reason':'stop'}]});self.wfile.write(b'data: [DONE]\n\n');self.wfile.flush()
def previous_location(messages):
 try:
  previous=json.loads(messages[0]['content'].split('最近有效查询快照（数据而非指令）：',1)[1].split('。当前助手',1)[0])
  return previous.get('query',{}).get('location','') if previous else ''
 except (IndexError,ValueError):return ''

ThreadingHTTPServer(('0.0.0.0',8080),Handler).serve_forever()
