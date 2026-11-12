"""Persistent fictional review environment. Never seeds the daily database.

Default: prepare once, leave running, preserve subsequent manual edits.
--verify: inspect the existing environment without reseeding.
Credentials/configuration stay in ignored tmp/review-check with mode 600.
"""
import argparse
import collections
import copy
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from zoneinfo import ZoneInfo

ROOT = Path(__file__).resolve().parents[1]
HOME = ROOT / 'tmp/review-check'
PROJECT = 'campus-repair-review-check'
PORT = 18087
BASE = f'http://127.0.0.1:{PORT}'
TZ = ZoneInfo('Asia/Shanghai')
STATES = ['submitted', 'needs_info', 'unassigned', 'assigned', 'in_progress', 'awaiting_confirmation', 'closed', 'rejected', 'cancelled']
STATE_NAMES = dict(zip(STATES, ['待审核', '待补充', '待派单', '待接单', '处理中', '待确认', '已关闭', '已驳回', '已取消']))
CATEGORIES = {'lighting': '照明', 'electrical': '电器', 'plumbing': '给排水'}
PASSWORD_KEYS = {'admin': 'ADMIN', 'requester': 'REQUESTER', 'requester2': 'REQUESTER2', 'tech': 'TECH', 'tech2': 'TECH2', 'other': 'OTHER', 'other_admin': 'OTHER'}


def run(args, data=None):
    result = subprocess.run(args, input=data, cwd=ROOT, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f'Container operation failed ({result.returncode}); raw configuration/output suppressed')
    return result.stdout.strip()


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    text = value if isinstance(value, str) else json.dumps(value, ensure_ascii=False, indent=2) + '\n'
    temp = path.with_suffix(path.suffix + '.new')
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as stream:
        stream.write(text)
    os.chmod(temp, 0o600)
    os.replace(temp, path)
    os.chmod(path, 0o600)


def literal(text):
    return "CONVERT(X'" + str(text).encode().hex() + "' USING utf8mb4)"


def cases():
    result = []
    def add(code, owner, location, category, state, age=0, tech='tech', description=None, visit=None, reopen=False):
        result.append(dict(code=code, owner=owner, location=location, category=category, state=state, age=age, tech=tech, description=description or {'lighting': '走廊灯不亮', 'electrical': '教室插座无法供电', 'plumbing': '洗手池水管漏水'}[category], visit=visit, reopen=reopen))
    add('Q1', 'requester', '研发楼一楼', 'lighting', 'assigned', 14, description='灯不亮')
    add('Q2', 'requester', '研发楼二楼', 'plumbing', 'in_progress', 14, 'tech2', '水管漏水')
    add('Q3', 'requester', '研发楼二楼', 'electrical', 'closed', 2, 'tech2', '空调不制冷')
    add('Q4', 'requester2', '研发楼一楼', 'lighting', 'assigned', description='灯闪烁')
    add('Q5', 'other', '研发楼一楼', 'lighting', 'submitted', description='隔离组织灯不亮，不应出现在本组织结果')
    add('Q6', 'requester', '测试%_楼201', 'plumbing', 'submitted')
    n = 0
    for state in STATES:
        for category in CATEGORIES:
            for owner in ('requester', 'requester2'):
                n += 1
                building = ['教学楼', '图书馆', '实验楼', '行政楼', '体育馆', '食堂'][n % 6]
                add(f'S{n:02}', owner, f'{building}{1+n%4}层{100+n}室', category, state, [0, 2, 8, 14][n % 4], 'tech' if n % 2 else 'tech2')
    visits = ['unscheduled', 'availability', 'scheduled', 'departed', 'arrived', 'no_show', 'waiting_parts', 'no_show_parts', 'overdue', 'overdue_departed']
    for n, visit in enumerate(visits, 1):
        add(f'V{n:02}', 'requester' if n % 2 else 'requester2', f'实训楼{n}层上门验收点', ['lighting', 'plumbing', 'electrical'][n % 3], 'in_progress', tech='tech' if n % 2 else 'tech2', visit=visit)
    add('E01', 'requester', '图书馆长文本验收区', 'lighting', 'submitted', description='长文本展示样本。' + '灯具偶尔闪烁，请核对位置与现象。' * 95)
    add('E02', 'requester2', '实验楼东区💡201', 'electrical', 'needs_info', description='含中文、emoji及换行的虚构样本。\n插座间歇断电，不自行拆卸。')
    add('E03', 'requester', '行政楼重处理验收区', 'plumbing', 'unassigned', reopen=True)
    for n in range(1, 8):
        add(f'X{n:02}', 'other', f'教学楼{n}层隔离验收点', list(CATEGORIES)[n % 3], ['submitted', 'needs_info', 'unassigned', 'rejected', 'cancelled'][n % 5])
    assert len(result) == 80 and sum(item['owner'] != 'other' for item in result) == 72
    return result


class Review:
    def __init__(self, verify_only=False):
        self.config_path = HOME/'compose.json'
        self.state_path = HOME/'manifest.json'
        self.verify_only = verify_only
        HOME.mkdir(parents=True, exist_ok=True)
        os.chmod(HOME, 0o700)
        if not self.state_path.exists():
            if verify_only:
                raise RuntimeError('Review dataset has not been prepared')
            self.create_environment()
        self.state = json.loads(self.state_path.read_text())
        if self.state.get('project') != PROJECT or self.state.get('version') != 1 or not self.config_path.exists():
            raise RuntimeError('Unknown/missing local fixture state; refusing replacement')
        self.config = json.loads(self.config_path.read_text())
        if self.config.get('name') != PROJECT:
            raise RuntimeError('Wrong Compose project')
        self.compose = ['docker', 'compose', '-p', PROJECT, '--project-directory', str(ROOT), '-f', str(self.config_path)]
        self.tokens, self.users = {}, {}
        self.anchor = dt.datetime.fromisoformat(self.state['created_at'])

    def create_environment(self):
        if self.config_path.exists():
            raise RuntimeError('Unrecognized existing local Compose file; left untouched')
        for kind in ('container', 'volume', 'network'):
            args = ['docker', 'ps', '-aq'] if kind == 'container' else ['docker', kind, 'ls', '-q']
            if run(args + ['--filter', 'label=com.docker.compose.project=' + PROJECT]):
                raise RuntimeError('Existing project resources without fixture manifest; refusing reuse')
        with socket.socket() as probe:
            if probe.connect_ex(('127.0.0.1', PORT)) == 0:
                raise RuntimeError('Review port occupied; no existing service was changed')
        source = json.loads(run(['docker', 'compose', 'config', '--format', 'json']))
        config = copy.deepcopy(source)
        config['name'] = PROJECT
        seed = {key: secrets.token_urlsafe(18) for key in source['services']['api']['environment'] if key.startswith('SEED_')}
        local = {**seed, 'DB_PASSWORD': secrets.token_hex(24), 'SESSION_SECRET': secrets.token_hex(32), 'MCP_CALL_SECRET': secrets.token_hex(32)}
        for group in ('networks', 'volumes'):
            for value in config.get(group, {}).values():
                if value.get('external'):
                    raise RuntimeError('External storage/network is not allowed for review')
                value.pop('name', None)
        for name, service in config['services'].items():
            if service.get('container_name'):
                raise RuntimeError('Fixed container names would collide')
            service.pop('build', None)
            for volume in service.get('volumes', []):
                if volume.get('type') != 'volume':
                    raise RuntimeError('Source configuration contains host storage; review must use independent volumes')
            if name in ('api', 'worker', 'mcp', 'web'):
                service['image'] = json.loads(run(['docker', 'image', 'inspect', service['image']]))[0]['Id']
                service['pull_policy'] = 'never'
            environment = service.get('environment', {})
            environment.update({key: value for key, value in local.items() if key in environment})
            if name == 'db':
                environment['MYSQL_PASSWORD'] = local['DB_PASSWORD']
                environment['MYSQL_ROOT_PASSWORD'] = secrets.token_hex(24)
            service['environment'] = {key: str(value).replace('$', '$$') for key, value in environment.items()}
        config['services']['web']['ports'] = [f'127.0.0.1:{PORT}:80']
        save(self.config_path, config)
        save(self.state_path, {'version': 1, 'project': PROJECT, 'created_at': dt.datetime.now(TZ).isoformat(), 'complete': False, 'cases': {}, 'materials': {}})
        accounts = ['独立人工验收环境（虚构数据）', BASE, '这些密码只用于本环境，不要提交或贴进聊天。', '']
        for name, key in PASSWORD_KEYS.items():
            accounts.append(name + '  ' + seed['SEED_' + key + '_PASSWORD'])
        save(HOME/'accounts.txt', '\n'.join(accounts) + '\n')

    def dc(self, *args, data=None):
        return run(self.compose + list(args), data)

    def persist(self):
        save(self.state_path, self.state)

    def guard(self):
        for service in ('api', 'db', 'worker', 'mcp'):
            cid = self.dc('ps', '-q', service)
            if not cid:
                raise RuntimeError('Review service missing: ' + service)
            container = json.loads(run(['docker', 'inspect', cid]))[0]
            if container['Config']['Labels'].get('com.docker.compose.project') != PROJECT:
                raise RuntimeError('Refusing non-review runtime')
            for mount in container['Mounts']:
                if mount['Type'] != 'volume' or not mount.get('Name', '').startswith(PROJECT + '_'):
                    raise RuntimeError('Refusing shared or host data mount')
            if service == 'api':
                environment = dict(value.split('=', 1) for value in container['Config']['Env'])
                if environment.get('DB_HOST') != 'db:3306':
                    raise RuntimeError('Refusing external database')

    def request(self, method, path, user=None, body=None, expected=200, headers=None, raw=None):
        hs = {'Content-Type': 'application/json', **(headers or {})}
        if user:
            hs['Authorization'] = 'Bearer ' + self.tokens[user]
        data = raw if raw is not None else json.dumps(body).encode() if body is not None else None
        try:
            with urllib.request.urlopen(urllib.request.Request(BASE + path, method=method, headers=hs, data=data), timeout=125) as response:
                status, result = response.status, json.load(response)
        except urllib.error.HTTPError as response:
            status = response.code
            result = {}
        if status != expected:
            raise RuntimeError(f'Review API failed: {method} {path.split("?")[0]} expected {expected}, got {status}; response suppressed')
        return result

    def sql(self, statement):
        self.guard()
        return self.dc('exec', '-T', 'db', 'sh', '-c', 'MYSQL_PWD="$MYSQL_PASSWORD" mysql --default-character-set=utf8mb4 -u "$MYSQL_USER" "$MYSQL_DATABASE" -N -B', data=statement)

    def start(self):
        self.dc('up', '-d', '--no-build')
        for _ in range(150):
            try:
                self.request('GET', '/health/ready')
                break
            except (OSError, RuntimeError):
                time.sleep(.4)
        else:
            raise RuntimeError('Review environment not ready')
        self.guard()
        passwords = self.config['services']['api']['environment']
        for user, key in PASSWORD_KEYS.items():
            login = self.request('POST', '/api/login', body={'username': user, 'password': passwords['SEED_' + key + '_PASSWORD']})
            self.tokens[user], self.users[user] = login['access_token'], login['user']

    def get(self, case):
        return self.request('GET', f"/api/work-orders/{self.state['cases'][case['code']]['id']}", case['owner'])['order']

    def action(self, case, action, user, **body):
        order = self.get(case)
        result = self.request('POST', f"/api/work-orders/{order['id']}/{action}", user, {'version': order['version'], **body})['order']
        self.state['cases'][case['code']]['version'] = result['version']
        self.persist()
        return result

    def visit(self, case, action, **body):
        order = self.get(case)
        user = case['owner'] if action == 'availability' else case['tech']
        path = 'availability' if action == 'availability' else 'actions/' + action
        self.request('PUT' if action == 'availability' else 'POST', f"/api/work-orders/{order['id']}/visit/{path}", user, {'version': order['version'], **body})
        self.state['cases'][case['code']]['version'] = self.get(case)['version']
        self.persist()

    def seed_order(self, case):
        code = case['code']
        saved = self.state['cases'].get(code)
        if saved and saved.get('ready'):
            return
        if not saved:
            marker = f'【虚构验收 {code}】'
            found = self.sql(f'SELECT id FROM work_orders WHERE LOCATE({literal(marker)},description)=1;')
            if found:
                if len(found.splitlines()) != 1:
                    raise RuntimeError('Duplicate fixture marker; no automatic repair')
                order = self.request('GET', '/api/work-orders/' + found, case['owner'])['order']
            else:
                key = hashlib.sha256((PROJECT + ':' + code).encode()).hexdigest()
                order = self.request('POST', '/api/work-orders', case['owner'], {'category': case['category'], 'location': case['location'], 'description': marker + case['description']}, expected=201, headers={'Idempotency-Key': key})['order']
            self.state['cases'][code] = {'id': order['id'], 'number': order['number'], 'version': order['version'], 'ready': False}
            self.persist()
        elif self.get(case)['version'] != saved['version']:
            raise RuntimeError('Partially prepared case was modified; preserved for manual inspection: ' + code)
        admin = 'other_admin' if case['owner'] == 'other' else 'admin'
        target = 'awaiting_confirmation' if case['reopen'] else case['state']
        for _ in range(8):
            order = self.get(case)
            if order['status'] == target:
                break
            if order['status'] == 'submitted':
                if target == 'cancelled':
                    self.action(case, 'cancel', case['owner'], reason='虚构验收：报修人主动取消')
                else:
                    decision = {'needs_info': 'request_info', 'rejected': 'reject'}.get(target, 'approve')
                    self.action(case, 'review', admin, decision=decision, reason='虚构验收：请补充具体现象' if target == 'needs_info' else '虚构验收：重复或不属范围，保留驳回依据')
            elif order['status'] == 'unassigned':
                self.action(case, 'assign', admin, technician_id=self.users[case['tech']]['id'])
            elif order['status'] == 'assigned':
                self.action(case, 'accept', case['tech'])
            elif order['status'] == 'in_progress':
                self.action(case, 'complete', case['tech'], measures='虚构验收：检查并更换对应部件', result='虚构验收：现场复测完成，等待本人确认')
            elif order['status'] == 'awaiting_confirmation' and target == 'closed':
                self.action(case, 'confirm', case['owner'])
            else:
                raise RuntimeError('Unexpected case state; preserved: ' + code)
        else:
            raise RuntimeError('Fixture transition limit reached')
        if case['reopen']:
            self.action(case, 'reopen', case['owner'], reason='虚构验收：处理后仍漏水，申请重新安排')
        if case['state'] == 'in_progress' and not case['visit']:
            self.action(case, 'records', case['tech'], measures='虚构验收：初步检查并隔离故障点', result='处理中，待进一步核对')
        if case['visit']:
            self.seed_visit(case)
        else:
            self.backdate(case, case['age'])
        self.state['cases'][code]['ready'] = True
        self.persist()
        print('Prepared ' + code + ' ' + STATE_NAMES[case['state']], flush=True)

    def backdate(self, case, days):
        order = self.get(case)
        day = self.anchor.replace(hour=0, minute=0, second=0, microsecond=0) - dt.timedelta(days=days)
        stamp = day + dt.timedelta(hours=9) if days else day + (self.anchor - day) / 2
        prefix = 'RP' + stamp.strftime('%Y%m%d')
        stamp = stamp.astimezone(dt.timezone.utc).strftime('%Y-%m-%d %H:%M:%S.%f')
        number = int(order['id'])
        guard = f'id={number} AND LOCATE({literal("【虚构验收 " + case["code"] + "】")},description)=1'
        self.sql(f"START TRANSACTION; UPDATE work_orders SET created_at={literal(stamp)},number=CONCAT({literal(prefix)},SUBSTRING(number,11)) WHERE {guard}; UPDATE work_order_events SET created_at=DATE_ADD({literal(stamp)},INTERVAL id MICROSECOND) WHERE work_order_id={number}; UPDATE repair_records SET created_at=COALESCE((SELECT MIN(e.created_at) FROM work_order_events e WHERE e.work_order_id={number} AND e.action IN ('records','complete')),{literal(stamp)}) WHERE work_order_id={number}; UPDATE audit_events SET created_at=DATE_ADD({literal(stamp)},INTERVAL id MICROSECOND) WHERE resource_id={number} AND action LIKE 'work_order.%'; UPDATE work_orders SET updated_at=(SELECT MAX(e.created_at) FROM work_order_events e WHERE e.work_order_id={number}) WHERE {guard}; UPDATE work_order_visits SET departed_at=(SELECT MAX(e.created_at) FROM work_order_events e WHERE e.work_order_id={number} AND e.action='visit.depart') WHERE work_order_id={number} AND stage='departed'; COMMIT;")
        self.state['cases'][case['code']]['number'] = prefix + order['number'][10:]
        self.persist()

    def seed_visit(self, case):
        kind = case['visit']
        if kind == 'unscheduled':
            return
        if kind == 'waiting_parts':
            self.visit(case, 'wait-parts', reason='虚构验收：等待专用配件', expected_resume_at=(dt.datetime.now(TZ) + dt.timedelta(days=2)).isoformat())
            return
        now = dt.datetime.now(TZ)
        start = (now + dt.timedelta(minutes=45)).replace(second=0, microsecond=0)
        if kind == 'availability':
            start = (now + dt.timedelta(days=1)).replace(hour=10, minute=0, second=0, microsecond=0)
        window = {'start': start.isoformat(), 'end': (start + dt.timedelta(hours=1)).isoformat()}
        self.visit(case, 'availability', **window)
        if kind == 'availability':
            return
        self.visit(case, 'schedule', **window)
        if kind in ('departed', 'arrived', 'no_show', 'no_show_parts', 'overdue_departed'):
            self.visit(case, 'depart')
        if kind in ('arrived', 'no_show', 'no_show_parts'):
            self.visit(case, 'arrive')
        if kind in ('no_show', 'no_show_parts'):
            self.visit(case, 'no-show', reason='虚构验收：到场敲门后无人应答，需要本人重新给时间')
        if kind == 'no_show_parts':
            self.visit(case, 'wait-parts', reason='虚构验收：白跑后发现还需备件，恢复时仍须重新上门', expected_resume_at=(now + dt.timedelta(days=2)).isoformat())
        if kind in ('overdue', 'overdue_departed'):
            order = self.get(case)
            past = (now - dt.timedelta(days=1)).replace(hour=14, minute=0, second=0, microsecond=0)
            end = past + dt.timedelta(hours=1)
            timestamp = lambda value: literal(value.astimezone(dt.timezone.utc).strftime('%Y-%m-%d %H:%M:%S'))
            vid = self.sql(f"SELECT active_visit_id FROM work_order_visit_context WHERE work_order_id={int(order['id'])};")
            if not vid.isdigit():
                raise RuntimeError('Missing fixture appointment')
            self.sql(f"UPDATE work_order_visit_context SET availability_start={timestamp(past)},availability_end={timestamp(end)} WHERE work_order_id={int(order['id'])}; UPDATE work_order_visits SET starts_at={timestamp(past)},ends_at={timestamp(end)},departed_at=" + (timestamp(past) if kind == 'overdue_departed' else 'NULL') + f" WHERE id={int(vid)} AND work_order_id={int(order['id'])}; UPDATE work_order_events SET note=REPLACE(REPLACE(note,{literal(start.strftime('%Y-%m-%d %H:%M'))},{literal(past.strftime('%Y-%m-%d %H:%M'))}),{literal((start+dt.timedelta(hours=1)).strftime('%Y-%m-%d %H:%M'))},{literal(end.strftime('%Y-%m-%d %H:%M'))}) WHERE work_order_id={int(order['id'])} AND action LIKE 'visit.%';")
            self.backdate(case, 1)

    def materials(self):
        definitions = [
            ('M1', 'admin', '【虚构验收】报修信息与上门说明', '这份材料仅供软件验收，不是校方制度。报修应提供具体楼栋、房间或公共区域位置、照明/电器/给排水类别和故障现象。方便上门时间在建单后补充，偏好不是预约，维修人员接单后人工确认。没有无人进入授权，不承诺短信通知。'),
            ('M2', 'admin', '【虚构验收】安全提醒', '仅供虚构软件验收。发现灯具冒烟、设备异味或漏水接近电器时，停止使用并联系现场工作人员，在确保自身安全的前提下远离故障区域；不要自行拆卸、接线或带电维修。本材料没有规定维修完成时限。'),
            ('M3', 'admin', '【虚构验收】管理员协调说明', '仅供管理员验收，演示识别词为蓝杉十七。待审核、待派单属于管理员待办。维修人员负载为待接单加处理中。白跑次数按到场后无人记录统计，不等于白跑下降比例。'),
            ('M4', 'other_admin', '【虚构验收】隔离组织专用材料', '本材料仅属于隔离测试组织，演示识别词为银杏九号。其他组织的查询和引用不应包含本段内容。不是校方制度。'),
        ]
        for code, user, title, text in definitions:
            if code in self.state['materials']:
                continue
            existing = self.request('GET', '/api/materials?page_size=100', user)['items']
            found = [item for item in existing if item['title'] == title]
            if found:
                mid = found[0]['id']
            else:
                boundary = 'review' + secrets.token_hex(12)
                raw = (f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n{title}\r\n--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="review-{code}.md"\r\n\r\n{text}\r\n--{boundary}--\r\n').encode()
                mid = self.request('POST', '/api/materials', user, expected=201, headers={'Content-Type': 'multipart/form-data; boundary=' + boundary}, raw=raw)['id']
            self.state['materials'][code] = mid
            self.persist()
        for admin in ('admin', 'other_admin'):
            for agent in self.request('GET', '/api/agents?manage=1', admin)['items']:
                mids = [self.state['materials']['M4']] if admin == 'other_admin' else [self.state['materials'][key] for key in ('M1', 'M2')] + ([self.state['materials']['M3']] if agent['role'] == 'admin' else [])
                tools = list(set(agent['tool_names']) | {'query_work_orders', {'admin': 'list_dispatch_candidates', 'requester': 'get_my_work_orders', 'technician': 'get_assigned_work_orders'}[agent['role']]})
                self.request('PUT', f"/api/agents/{agent['id']}", admin, {**agent, 'name': '【验收】' + agent['name'].removeprefix('【验收】'), 'material_ids': mids, 'tool_names': sorted(tools)})
        deadline = time.monotonic() + 75
        while time.monotonic() < deadline:
            statuses = [self.request('GET', f'/api/materials/{self.state["materials"][code]}', user)['material']['status'] for code, user, _, _ in definitions]
            if all(status not in ('pending', 'processing') for status in statuses):
                break
            time.sleep(1)

    def inspect(self, initial=False):
        reports = {user: self.request('GET', '/api/work-orders/summary', user) for user in PASSWORD_KEYS}
        if initial:
            assert reports['admin']['total'] == 72 and reports['other_admin']['total'] == 8, 'Wrong initial organization counts'
            assert all(reports['admin']['statuses'].get(state, 0) > 0 for state in STATES), 'Missing state coverage'
        q1 = self.state['cases']['Q1']['id']
        for user in ('requester2', 'tech2', 'other', 'other_admin'):
            self.request('GET', f'/api/work-orders/{q1}', user, expected=404)
        self.request('GET', f'/api/work-orders/{self.state["cases"]["Q5"]["id"]}', 'admin', expected=404)
        whole = self.request('GET', '/api/work-orders?location=' + urllib.parse.quote('研发楼'), 'admin')
        if initial:
            assert whole['total'] == 4, 'Research building fixture polluted'
        first = self.request('GET', '/api/work-orders?page=1&page_size=20', 'admin')
        second = self.request('GET', '/api/work-orders?page=2&page_size=20', 'admin')
        assert len(first['items']) == 20 and len(second['items']) == 20
        assert not {item['id'] for item in first['items']} & {item['id'] for item in second['items']}, 'Page overlap'
        metrics = self.request('GET', '/api/repair-workbench/metrics', 'admin')
        workbenches = {user: self.request('GET', '/api/repair-workbench', user)['counts'] for user in ('admin', 'tech', 'tech2')}
        if initial:
            assert workbenches['admin']['overdue'] == 2 and workbenches['admin']['reschedule'] == 1 and workbenches['admin']['parts'] == 2, 'Wrong visit scenarios'
            assert metrics['counts']['no_show'] == 2, 'Wrong recorded no-show count'
        material_status = {}
        for code, mid in self.state['materials'].items():
            material = self.request('GET', f'/api/materials/{mid}', 'other_admin' if code == 'M4' else 'admin')['material']
            material_status[code] = {key: material.get(key) for key in ('status', 'generation', 'vector_ready')}
        result = {'as_of': dt.datetime.now(TZ).isoformat(), 'initial': initial, 'summaries': reports, 'workbenches': workbenches, 'metrics': metrics, 'materials': material_status, 'research_total': whole['total']}
        save(HOME/'current-check.json', result)
        return result

    def report(self, result):
        lines = ['# 人工验收数据对照', '', '全部为虚构数据。生成于：' + result['as_of'], '', '入口：' + BASE, '账号密码仅在同目录accounts.txt，不要贴进聊天或提交。', '', '## 初始查询对照', '', '| 管理员问题 | 初始预期 |', '| --- | --- |', '| 研发楼有什么问题 | 4单，Q1/Q2/Q3/Q4，不限日期 |', '| 研发楼一楼怎么了 | 2单，Q1/Q4 |', '| 研发楼有哪些未解决问题 | 3单，Q1/Q2/Q4 |', '| 研发楼最近有什么故障 | 2单，Q3/Q4，按创建时间 |', '| 查测试%_楼 | 1单，Q6，百分号与下划线按字面 |', '', '## 当前角色概览', '', '| 账号 | 全部 | 待我处理 | 未结束 | 已结束 |', '| --- | --- | --- | --- | --- |']
        for user, summary in result['summaries'].items():
            lines.append(f"| {user} | {summary['total']} | {summary['action']} | {summary['active']} | {summary['finished']} |")
        lines += ['', '## 上门协调初始值', '', '预约逾期2单、待重新约时1单、待备件2单；V06与V08各一次已记录白跑。数量随时间及人工操作变化。', '', '## 工单目录', '', '| 编号 | 真实工单号 | 报修人 | 位置 | 初始状态 | 场景 |', '| --- | --- | --- | --- | --- | --- |']
        for case in cases():
            saved = self.state['cases'][case['code']]
            lines.append(f"| {case['code']} | {saved['number']} | {case['owner']} | {case['location']} | {STATE_NAMES[case['state']]} | {case['visit'] or ('重处理' if case['reopen'] else str(case['age'])+'天前创建')} |")
        lines += ['', '## 操作入口', '', '- 用admin核对计数、审核、派单、改派与上门协调。', '- 用requester/requester2处理本人待补充、待确认、补上门时间和AI草稿。', '- 用tech/tech2在工作台接单、预约、出发、到场、反馈异常。', '- V01无安排；V02仅方便时间；V03已预约；V04已出发；V05已到场；V06白跑待重约；V07待备件；V08白跑后待备件；V09预约逾期；V10已出发且逾期。', '- M1/M2三角色可检索，M3仅管理员绑定，M4属于隔离组织；所有材料均标明虚构。', '- 测试日期为生成时相对日期，翌日今日/逾期队列会变化。', '- 初始预期仅在尚未修改数据时成立；重复运行保留人工修改，不自动重置。', '- 不要在本环境运行会批量建单或故障注入的自动回归，避免破坏你的审核基线。', '']
        save(HOME/'guide.md', '\n'.join(lines))

    def prepare(self):
        self.start()
        if self.verify_only or self.state['complete']:
            result = self.inspect()
            print('Existing data and manual changes preserved. Current summaries inspected.', flush=True)
        else:
            for case in cases():
                self.seed_order(case)
            self.materials()
            result = self.inspect(initial=True)
            self.report(result)
            self.state['complete'] = True
            self.persist()
            print('Prepared 80 fictional orders (72 + 8), 10 visit scenarios and 4 materials.', flush=True)
        print('Review URL: ' + BASE, flush=True)
        print('Local accounts: ' + str(HOME/'accounts.txt'), flush=True)
        print('Dataset guide: ' + str(HOME/'guide.md'), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verify', action='store_true', help='Inspect existing review data without reseeding')
    arguments = parser.parse_args()
    Review(arguments.verify).prepare()
