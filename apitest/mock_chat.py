"""Streaming model fixture. Only fictional isolated test input is recorded."""
import json,time
from pathlib import Path
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  try:self.respond()
  except (BrokenPipeError,ConnectionResetError):pass
 def respond(self):
  body=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  with Path('/state/chat-requests.jsonl').open('a') as f:f.write(json.dumps(body,ensure_ascii=False)+'\n')
  mode=Path('/state/chat-mode').read_text().strip() if Path('/state/chat-mode').exists() else 'ok'
  if mode=='fail':self.send_response(503);self.end_headers();self.wfile.write(b'upstream-private-diagnostic');return
  self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
  def event(value):self.wfile.write(('data: '+json.dumps(value,ensure_ascii=False)+'\n\n').encode());self.wfile.flush()
  def delta(text):event({'choices':[{'delta':{'content':text},'finish_reason':None}]})
  if mode in ('slow','timeout','disconnect'):
   delta('正在核对报修要求。')
   if mode=='disconnect':return
   time.sleep(2 if mode=='slow' else 6)
  if mode=='malformed':self.wfile.write(b'data: {bad\n\n');self.wfile.flush();return
  if mode=='tools':event({'choices':[{'delta':{'tool_calls':[{'function':{'name':'assign'}}]},'finish_reason':'tool_calls'}]});return
  if mode=='unsafe':answer='第一步拆开配电箱并接线。'
  elif mode=='fake':answer='报修需要填写位置。[999] 也要描述故障现象。[1]'
  elif mode=='unused':answer='请填写具体位置。'
  else:answer='报修时请填写楼栋、房间和故障现象。[1]'
  for start in range(0,len(answer),3):delta(answer[start:start+3])
  event({'choices':[{'delta':{},'finish_reason':'stop'}]});self.wfile.write(b'data: [DONE]\n\n');self.wfile.flush()
ThreadingHTTPServer(('0.0.0.0',8080),Handler).serve_forever()
