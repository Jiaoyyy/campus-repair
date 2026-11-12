"""Four real native-plan queries against fictional old orders in a fresh check environment."""
import json
import time
import uuid
from verify_gateway import Verification, ROOT


class BuildingVerification(Verification):
    def verify(self):
        self.dc('up', '-d', '--no-build')
        for _ in range(120):
            try:
                self.request('GET', '/health/ready')
                break
            except (RuntimeError, OSError):
                time.sleep(.5)
        tokens = {name: self.request('POST', '/api/login', body={'username': name, 'password': self.passwords[key]})['access_token'] for name, key in [('admin', 'SEED_ADMIN_PASSWORD'), ('requester', 'SEED_REQUESTER_PASSWORD'), ('other', 'SEED_OTHER_PASSWORD')]}
        agent = self.request('GET', '/api/agents', tokens['admin'])['items'][0]['id']
        orders = []
        for name, location in [('requester', '研发楼一楼'), ('requester', '研发楼二楼'), ('requester', '研发楼三楼'), ('other', '研发楼一楼')]:
            order = self.request('POST', '/api/work-orders', tokens[name], {'category': 'lighting', 'location': location, 'description': '虚构验收灯不亮'}, 201, {'Idempotency-Key': uuid.uuid4().hex})['order']
            orders.append(order)
        ids = ','.join(str(int(order['id'])) for order in orders)
        sql = f"UPDATE work_orders SET created_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 14 DAY) WHERE id IN ({ids}); UPDATE work_orders SET status='closed' WHERE id={int(orders[2]['id'])};"
        self.dc('exec', '-T', 'db', 'sh', '-c', 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -e "$1"', 'fixture', sql)
        before = self.request('GET', '/api/work-orders/summary', tokens['admin'])
        self.evidence['model'] = self.gateway['CHAT_MODEL']
        self.evidence['queries'] = []
        cases = [('研发楼有什么问题', 'all', 3), ('研发楼一楼怎么了', 'all', 1), ('研发楼最近有什么故障', 'recent', 0), ('研发楼有哪些未解决问题', 'all', 2)]
        for question, period, total in cases:
            _, meta, done = self.turn(agent, tokens['admin'], question)
            result = done.get('business', {})
            self.check(result.get('kind') == 'orders' and result.get('total') == total, '真实旧工单结果：' + question)
            self.check(result['query'].get('period', 'all') == period and not result['query'].get('keyword'), '真实计划日期及泛称范围：' + question)
            audit = self.request('GET', '/api/audit?page_size=100', tokens['admin'])['items']
            self.check(sum(item['request_id'] == meta['request_id'] and item['action'] == 'mcp.call' and item['outcome'] == 'success' for item in audit) == 1, '本轮一次授权MCP查询')
            if period == 'recent':
                self.check('时间范围外仍可能有未结束工单' in done['answer'], '近期空结果说明旧工单范围')
            if '未解决' in question:
                self.check(result['query'].get('queue') == 'active', '当前未解决不受创建日期影响')
            self.evidence['queries'].append({'question': question, 'request_id': meta['request_id'], 'result': result})
        self.check(before == self.request('GET', '/api/work-orders/summary', tokens['admin']), '真实查询不改变业务')


if __name__ == '__main__':
    v = BuildingVerification(18085)
    try:
        v.verify()
        v.evidence['result'] = 'passed'
    finally:
        v.dc('down')
        path = ROOT/'tmp'/(v.project + '-building.json')
        path.write_text(json.dumps(v.evidence, ensure_ascii=False, indent=2) + '\n')
    print('真实楼栋范围专项通过；仅四个原生计划问题，证据：' + str(path))
