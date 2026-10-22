"""真实网关集成验收；使用新建隔离环境与虚构材料，会产生少量模型调用费用。"""
import argparse
import copy
import datetime
import json
import secrets
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from zoneinfo import ZoneInfo

ROOT = Path(__file__).resolve().parents[1]
GATEWAY_KEYS = (
    'CHAT_BASE_URL', 'CHAT_API_KEY', 'CHAT_MODEL',
    'EMBEDDING_BASE_URL', 'EMBEDDING_API_KEY', 'EMBEDDING_MODEL',
    'EMBEDDING_DIMENSION',
)


def run(argv, data=None):
    result = subprocess.run(argv, input=data, cwd=ROOT, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f'容器操作失败（退出码 {result.returncode}）；未输出原始配置或错误正文')
    return result.stdout


class Verification:
    def __init__(self, port):
        source = json.loads(run(['docker', 'compose', 'config', '--format', 'json']))
        self.gateway = source['services']['api']['environment']
        if not all(self.gateway.get(key) for key in GATEWAY_KEYS):
            raise RuntimeError('真实网关必需变量不齐全')
        if any('mock' in str(self.gateway[key]).lower() for key in GATEWAY_KEYS):
            raise RuntimeError('拒绝将模拟网关记录为真实验收')
        self.project = 'campus-repair-gateway-' + secrets.token_hex(4) + '-check'
        resource_filter = 'label=com.docker.compose.project=' + self.project
        if (run(['docker', 'ps', '-aq', '--filter', resource_filter]).strip()
                or run(['docker', 'volume', 'ls', '-q', '--filter', resource_filter]).strip()
                or run(['docker', 'network', 'ls', '-q', '--filter', resource_filter]).strip()):
            raise RuntimeError('随机项目名已有资源；拒绝复用或清理已有环境，请重新运行')
        self.base = f'http://127.0.0.1:{port}'
        self.checks = []
        self.evidence = {'date': datetime.datetime.now(ZoneInfo('Asia/Shanghai')).isoformat(),
                         'project': self.project, 'gateway': 'real', 'checks': self.checks}
        self.passwords = {key: secrets.token_hex(24) for key in self.gateway if key.startswith('SEED_')}
        local = {**self.passwords, 'DB_PASSWORD': secrets.token_hex(24),
                 'SESSION_SECRET': secrets.token_hex(32), 'MCP_CALL_SECRET': secrets.token_hex(32)}
        config = copy.deepcopy(source)
        config['name'] = self.project
        for group in ('volumes', 'networks'):
            for item in config.get(group, {}).values():
                if item.get('external'):
                    raise RuntimeError('验收配置不得引用外部卷或网络')
                item.pop('name', None)
        for name, service in config['services'].items():
            service.pop('build', None)
            if name in ('api', 'worker', 'mcp', 'web'):
                image = 'campus-repair-web:latest' if name == 'web' else 'campus-repair-api:latest'
                run(['docker', 'image', 'inspect', image])
                service['image'] = image
                service['pull_policy'] = 'never'
            env = service.get('environment', {})
            env.update({key: value for key, value in local.items() if key in env})
            if name == 'db':
                env['MYSQL_PASSWORD'] = local['DB_PASSWORD']
                env['MYSQL_ROOT_PASSWORD'] = secrets.token_hex(24)
            # 已解析环境再次通过 Compose 输入，转义字面美元符，避免二次插值。
            service['environment'] = {key: str(value).replace('$', '$$') for key, value in env.items()}
        config['services']['web']['ports'] = [f'127.0.0.1:{port}:80']
        self.config = json.dumps(config)
        self.compose = ['docker', 'compose', '-p', self.project, '-f', '-']
        self.secrets = [str(self.gateway[key]) for key in ('CHAT_API_KEY', 'EMBEDDING_API_KEY')]
        self.secrets += list(local.values()) + [config['services']['db']['environment']['MYSQL_ROOT_PASSWORD']]

    def dc(self, *args):
        return run(self.compose + list(args), self.config)

    def check(self, condition, label):
        if not condition:
            raise RuntimeError(label)
        self.checks.append(label)
        print('PASS ' + label, flush=True)

    def request(self, method, path, token=None, body=None, status=200, headers=None):
        hs = {'Content-Type': 'application/json', **(headers or {})}
        if token:
            hs['Authorization'] = 'Bearer ' + token
        data = json.dumps(body).encode() if isinstance(body, dict) else body
        request = urllib.request.Request(self.base + path, data=data, headers=hs, method=method)
        try:
            with urllib.request.urlopen(request, timeout=125) as response:
                code, data = response.status, json.load(response)
        except urllib.error.HTTPError as response:
            code, data = response.code, {}
        if code != status:
            raise RuntimeError(f'接口状态异常：{method} {path.split("?")[0]} HTTP {code}')
        return data

    def turn(self, agent, token, question, conversation_id=None):
        cid = conversation_id or self.request('POST', '/api/conversations', token, {'agent_id': agent}, 201)['conversation']['id']
        request = urllib.request.Request(self.base + f'/api/conversations/{cid}/messages',
                                        data=json.dumps({'content': question}).encode(),
                                        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
        events = []
        with urllib.request.urlopen(request, timeout=125) as response:
            kind, parts = '', []
            for raw in response:
                line = raw.decode().rstrip('\r\n')
                if line.startswith('event:'):
                    kind = line[6:].strip()
                elif line.startswith('data:'):
                    parts.append(line[5:].strip())
                elif not line and kind:
                    events.append((kind, json.loads('\n'.join(parts))))
                    kind, parts = '', []
        self.check(bool(events) and events[0][0] == 'meta' and events[-1][0] == 'done'
                   and sum(e[0] in ('done', 'error') for e in events) == 1,
                   'SSE 完整结束：' + question)
        done = events[-1][1]
        self.check(bool(done['answer'].strip()) and any(e[0] == 'delta' for e in events), '收到非空流式正文')
        history = self.request('GET', f'/api/conversations/{cid}/messages', token)['items']
        self.check(any(m['role'] == 'assistant' and m['status'] == 'complete'
                       and m['content'] == done['answer'] for m in history), '完成回答已持久化')
        return cid, events[0][1], done

    def verify(self):
        print('启动独立真实网关验收环境：' + self.project, flush=True)
        self.dc('up', '-d', '--no-build')
        for _ in range(120):
            try:
                self.request('GET', '/health/ready')
                break
            except (RuntimeError, OSError):
                time.sleep(.5)
        else:
            raise RuntimeError('独立环境未就绪')
        for name in ('api', 'worker', 'mcp'):
            cid = self.dc('ps', '-q', name).strip()
            actual = dict(x.split('=', 1) for x in json.loads(run(['docker', 'inspect', cid]))[0]['Config']['Env'])
            self.check(all(actual.get(k) == str(self.gateway[k]) for k in GATEWAY_KEYS), name + ' 已读取真实网关配置')

        tokens = {}
        for name, key in [('admin', 'SEED_ADMIN_PASSWORD'), ('requester', 'SEED_REQUESTER_PASSWORD'),
                          ('tech', 'SEED_TECH_PASSWORD'), ('requester2', 'SEED_REQUESTER2_PASSWORD'),
                          ('other', 'SEED_OTHER_PASSWORD')]:
            tokens[name] = self.request('POST', '/api/login', body={'username': name, 'password': self.passwords[key]})['access_token']
        self.secrets.extend(tokens.values())
        admin = tokens['admin']
        # Fresh role assistants have no materials: these calls must still reach the real model.
        own = self.request('GET', '/api/agents', tokens['requester'])['items'][0]['id']
        general = []
        cid, meta, done = self.turn(own, tokens['requester'], '你好，你可以怎么帮我？')
        self.check('报修' in done['answer'] and not done['citations'], '无材料也能自然介绍报修帮助')
        general.append({'question': '你好，你可以怎么帮我？', 'request_id': meta['request_id'], **done})
        cid, meta, done = self.turn(own, tokens['requester'], '宿舍灯坏了')
        self.check(any(word in done['answer'] for word in ('楼', '房间')) and not done['citations'], '无材料故障描述触发模型追问')
        general.append({'question': '宿舍灯坏了', 'request_id': meta['request_id'], **done})
        _, meta, done = self.turn(own, tokens['requester'], '三号楼201，今晚发现，灯完全不亮', cid)
        self.check(not any(word in done['answer'] for word in ('哪栋', '哪个房间')) and not done['citations'], '真实多轮对话保留已提供位置')
        general.append({'question': '三号楼201，今晚发现，灯完全不亮', 'request_id': meta['request_id'], **done})
        _, meta, done = self.turn(own, tokens['requester'], '学校规定报修后必须在两小时内修好吗？')
        self.check(not done['citations'] and '核实' in done['warning'], '无资料制度问题有明确未核实提示')
        general.append({'question': '学校规定报修后必须在两小时内修好吗？', 'request_id': meta['request_id'], **done})
        admin_agent = self.request('GET', '/api/agents', admin)['items'][0]['id']
        _, meta, done = self.turn(admin_agent, admin, '我们有几个师傅？')
        self.check(not meta.get('tool') and any(word in done['answer'] for word in ('哪类', '哪一类', '照明', '电器', '类别')), '自然人员问题缺少类别时追问，不伪造人数')
        general.append({'question': '我们有几个师傅？', 'request_id': meta['request_id'], **done})
        self.evidence['natural_dialogue'] = general
        text = '演示资料，不代表校方制度。\n照明报修请填写楼栋、房间、故障现象和发现时间。\n演示验收识别词：青竹灯塔。'
        boundary = 'gateway' + secrets.token_hex(8)
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n演示：真实网关联调须知\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="demo.md"\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
        mid = self.request('POST', '/api/materials', admin, body, 201,
                           {'Content-Type': 'multipart/form-data; boundary=' + boundary})['id']
        for _ in range(120):
            material = self.request('GET', f'/api/materials/{mid}', admin)['material']
            if material['status'] in ('ready', 'failed'):
                break
            time.sleep(.5)
        self.check(material['status'] == 'ready' and material['vector_ready'] and material['generation'] == 1,
                   '真实嵌入、Qdrant 写入与索引发布成功')
        agents = self.request('GET', '/api/agents?manage=1', admin)['items']
        tools = {'requester': 'get_my_work_orders', 'technician': 'get_assigned_work_orders', 'admin': 'list_dispatch_candidates'}
        discovered = self.request('GET', '/api/mcp/tools', admin)['items']
        self.check({x['name'] for x in discovered} == set(tools.values()), '内部 MCP 工具发现成功')
        by_role = {}
        for agent in agents:
            agent.update(enabled=True, material_ids=[mid], tool_names=[tools[agent['role']]])
            by_role[agent['role']] = self.request('PUT', f'/api/agents/{agent["id"]}', admin, agent)['agent']
        aid = by_role['requester']['id']
        for mode in ('keyword', 'vector', 'hybrid'):
            hits = self.request('GET', '/api/retrieval?' + urllib.parse.urlencode(
                {'agent_id': aid, 'mode': mode, 'q': '照明报修'}), tokens['requester'])['items']
            self.check(bool(hits) and all(h['material_id'] == mid for h in hits), mode + ' 检索命中绑定材料')

        answers = []
        for role, name in [('requester', 'requester'), ('technician', 'tech'), ('admin', 'admin')]:
            cid, meta, done = self.turn(by_role[role]['id'], tokens[name], '照明报修需要填写哪些信息？请根据演示资料简要回答并标注来源。')
            self.check(not meta.get('tool') and bool(done['citations']), role + ' 真实模型回答包含实际引用')
            self.check(all(word in done['answer'] for word in ('楼栋', '房间', '故障', '时间')), role + ' 回答涵盖材料四项要求')
            for cite in done['citations']:
                path = f'/api/conversations/{cid}/citations/{done["message_id"]}/{cite["chunk_id"]}'
                source = self.request('GET', path, tokens[name])
                self.check(cite['material_id'] == mid and source['text'][cite['start']:cite['end']] == cite['text'], '引用与原文字符范围一致')
                for other in ('requester2', 'other'):
                    self.request('GET', path, tokens[other], status=404)
                self.check(True, '同组织他人、跨组织用户不能访问私有来源')
            answers.append({'role': role, 'conversation_id': cid, 'request_id': meta['request_id'], **done})

        # 在隔离环境建一个已派单演示工单，让只读查询有真实业务记录可比对。
        order = self.request('POST', '/api/work-orders', tokens['requester'],
                             {'category': 'lighting', 'location': '演示楼201', 'description': '演示灯具不亮'}, 201,
                             {'Idempotency-Key': secrets.token_hex(16)})['order']
        order = self.request('POST', f'/api/work-orders/{order["id"]}/review', admin,
                             {'version': order['version'], 'decision': 'approve'})['order']
        tech_id = self.request('GET', '/api/me', tokens['tech'])['user']['id']
        order = self.request('POST', f'/api/work-orders/{order["id"]}/assign', admin,
                             {'version': order['version'], 'technician_id': tech_id})['order']
        mcp_evidence = []
        for role, name, question in [('requester', 'requester', '我的工单进度'),
                                     ('technician', 'tech', '我的任务'), ('admin', 'admin', '照明类可派人员有哪些')]:
            _, meta, done = self.turn(by_role[role]['id'], tokens[name], question)
            self.check(meta.get('tool') == tools[role], role + ' 经 MCP Client/Server 完成只读查询')
            if role != 'admin':
                self.check(order['number'] in done['answer'], role + ' 查询包含已派单演示工单')
            mcp_evidence.append({'role': role, 'request_id': meta['request_id'], 'tool': meta['tool'], 'answer': done['answer']})
        after = self.request('GET', f'/api/work-orders/{order["id"]}', admin)['order']
        self.check(after == order, 'MCP 查询未改变演示工单')
        audit = self.request('GET', '/api/audit?page_size=100', admin)['items']
        self.check(sum(a['action'] == 'mcp.call' and a['outcome'] == 'success' for a in audit) == 3, '三次 MCP 成功调用均有审计')

        logs = self.dc('logs', '--no-color', 'api', 'worker', 'mcp', 'web')
        cid = self.dc('ps', '-q', 'web').strip()
        assets = run(['docker', 'exec', cid, 'sh', '-c', 'find /usr/share/nginx/html -type f -exec cat {} +'])
        tracked = run(['git', 'grep', '-I', '-e', '.'])
        inspected = logs + assets + tracked + json.dumps(audit) + json.dumps(answers) + json.dumps(self.evidence.get('natural_dialogue', []))
        self.check(not any(value in inspected for value in self.secrets), '运行日志、前端静态包、已跟踪源码及本次审计/回答无秘密原文')
        self.evidence.update(material_id=mid, generation=material['generation'],
                             embedding_dimension=int(self.gateway['EMBEDDING_DIMENSION']),
                             rag=answers, mcp=mcp_evidence)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', type=int, default=18085)
    args = parser.parse_args()
    verification = Verification(args.port)
    try:
        verification.verify()
        verification.evidence['result'] = 'passed'
    finally:
        # 只停止本次新建项目；保留虚构数据卷，不接触已有环境。
        verification.dc('down')
    target = ROOT / 'tmp' / (verification.project + '.json')
    target.parent.mkdir(exist_ok=True)
    target.write_text(json.dumps(verification.evidence, ensure_ascii=False, indent=2) + '\n')
    print(f'真实网关联调通过：{len(verification.checks)} 项检查；证据：{target}')
    print('独立容器已停止，演示数据卷保留于项目：' + verification.project)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # 不输出 urllib 异常详情（可能含 URL），只展示本脚本的脱敏错误。
        print('验收失败：' + (str(error) if isinstance(error, RuntimeError) else type(error).__name__))
        raise SystemExit(1)
