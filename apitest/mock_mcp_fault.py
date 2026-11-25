"""Isolated MCP peer for testing the Host's deadline and protocol handling."""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import time


class Peer(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, status, data=b''):
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        self.reply(200 if self.path == '/health/live' else 405)

    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        method = request['method']
        if method == 'initialize':
            result = {'protocolVersion': request['params']['protocolVersion'],
                      'capabilities': {'tools': {}}, 'serverInfo': {'name': 'isolated-fault-peer', 'version': '1'}}
            self.reply(200, json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': result}).encode())
        elif method == 'notifications/initialized':
            self.reply(202)
        elif method == 'tools/list':
            names = ('query_work_orders', 'get_my_work_orders', 'get_assigned_work_orders',
                     'list_dispatch_candidates', 'get_work_order_detail', 'get_work_order_visit', 'search_repair_knowledge')
            tools = [{'name': name, 'inputSchema': {'type': 'object'}} for name in names]
            self.reply(200, json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': {'tools': tools}}).encode())
        elif method == 'tools/call':
            if Path('/tmp/mcp-fault-mode').read_text() == 'timeout':
                time.sleep(15)
            try:
                self.reply(200, b'{invalid MCP JSON')
            except (BrokenPipeError, ConnectionResetError):
                pass
        else:
            self.reply(405)


ThreadingHTTPServer(('0.0.0.0', 8081), Peer).serve_forever()
