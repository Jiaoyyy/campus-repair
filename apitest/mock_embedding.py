"""Isolated deterministic embedding fixture; never for gateway acceptance."""
import json,math,time
from http.server import BaseHTTPRequestHandler,HTTPServer
from pathlib import Path
class Handler(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  body=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  mode=Path('/state/mode').read_text().strip() if Path('/state/mode').exists() else 'ok'
  if mode=='fail': self.send_response(503);self.end_headers();return
  if mode=='slow':time.sleep(12)
  def vector(s):
   values=[float(s.count(word)) for word in ['照明','报修','水管','安全']]
   values[0]+=.01
   norm=math.sqrt(sum(x*x for x in values));return [x/norm for x in values]
  result={'data':[{'index':i,'embedding':vector(s)} for i,s in enumerate(body['input'])]}
  self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps(result).encode())
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
