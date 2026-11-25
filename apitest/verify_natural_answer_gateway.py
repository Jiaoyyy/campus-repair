"""Paid gateway smoke with small fictional fixtures and independent storage."""
import argparse
import json
import os
import secrets
from pathlib import Path
import time
import urllib.parse

from verify_gateway import Verification, run

def publish_lighting_material(v, admin, text):
    boundary = 'natural-' + secrets.token_hex(8)
    body = (
        f'--{boundary}\r\nContent-Disposition: form-data; name="title"\r\n\r\n虚构照明安全资料\r\n'
        f'--{boundary}\r\nContent-Disposition: form-data; name="demo"\r\n\r\ntrue\r\n'
        f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="safety.txt"\r\n'
        f'Content-Type: text/plain\r\n\r\n{text}\r\n--{boundary}--\r\n'
    ).encode()
    mid = v.request('POST', '/api/materials', admin, body, 201,
                    {'Content-Type': 'multipart/form-data; boundary=' + boundary})['id']
    for _ in range(120):
        material = v.request('GET', f'/api/materials/{mid}', admin)['material']
        if material['status'] in ('ready', 'failed'):
            break
        time.sleep(.5)
    v.check(material['generation'] > 0, 'fictional safety material has a published text index')
    return mid, material


def verify_safety(v, admin, tech, agent):
    text = '虚构验收材料：灯具闪烁时先停止使用故障灯具，并联系现场维修人员。不要自行拆卸、接线或带电维修。'
    mid, material = publish_lighting_material(v, admin, text)
    a = next(a for a in v.request('GET', '/api/agents?manage=1', admin)['items'] if a['id'] == agent)
    v.request('PUT', f'/api/agents/{agent}', admin, {**a, 'material_ids': [mid]})
    question = '灯具闪烁时有哪些安全自查注意事项？请依据演示资料并标明出处。'
    started = time.perf_counter()
    cid, meta, answer = v.turn(agent, tech, question)
    v.check(bool(answer.get('citations')), 'safety answer uses actual source citations')
    for cite in answer['citations']:
        source = v.request('GET', f'/api/conversations/{cid}/citations/{answer["message_id"]}/{cite["chunk_id"]}', tech)
        v.check(cite['material_id'] == mid and source['text'][cite['start']:cite['end']] == cite['text'],
                'citation opens the matching fictional passage')
    return {'question': question, 'request_id': meta['request_id'],
            'duration_ms': round((time.perf_counter()-started)*1000), 'answer': answer['answer'],
            'citations': answer['citations'], 'vector_ready': material['vector_ready']}

def verify_draft_advice(v, admin, requester, agent, records):
    before = v.request('GET', '/api/work-orders/summary', requester)
    question = '我们宿舍灯坏了，咋整？刚刚发现灯一直闪。'
    def take_turn(question, conversation=None):
        cid, meta, done = v.turn(agent, requester, question, conversation)
        draft = done.get('draft', {})
        row = {'question': question, 'request_id': meta['request_id'], 'answer': done['answer'],
               'draft': {k: val for k, val in draft.items() if k != 'idempotency_key'},
               'citations': done['citations'], 'message_id': done['message_id']}
        records.append(row)
        return cid, row
    _, generic = take_turn(question)
    v.check(generic['draft']['location'] == '' and not generic['citations'] and '规定' not in generic['answer'], 'no source: general guidance and partial draft')
    text = '虚构照明安全资料：宿舍灯具故障或一直闪烁时，先停止使用故障灯具，并联系宿管登记。不要自行拆卸或接线。'
    mid, _ = publish_lighting_material(v, admin, text)
    a = next(a for a in v.request('GET', '/api/agents?manage=1', admin)['items'] if a['id'] == agent)
    v.request('PUT', f'/api/agents/{agent}', admin, {**a, 'material_ids': [mid]})
    params = urllib.parse.urlencode({'agent_id': agent, 'mode': 'keyword', 'q': '照明 灯具 故障 安全'})
    hits = v.request('GET', '/api/retrieval?' + params, requester)['items']
    v.check(any(hit['material_id'] == mid for hit in hits), 'fixture material matched the intake retrieval query')
    cid, grounded = take_turn(question)
    v.check('咋整' not in grounded['draft']['description'] and '闪' in grounded['draft']['description'], 'draft summarizes the reported symptom without conversational filler')
    v.check(not grounded['draft']['location'] and ('哪栋' in grounded['answer'] or '楼栋' in grounded['answer']), 'only missing location is requested')
    v.check(grounded['citations'] and '宿管' in grounded['answer'], 'relevant document cited for fault guidance')
    for cite in grounded['citations']:
        source = v.request('GET', f'/api/conversations/{cid}/citations/{grounded["message_id"]}/{cite["chunk_id"]}', requester)
        v.check(cite['material_id'] == mid and source['text'][cite['start']:cite['end']] == cite['text'], 'source opens the bound safety passage')
    _, follow = take_turn('三号楼201，不是闪烁，现在完全不亮了。', cid)
    v.check(follow['draft']['location'] == '三号楼201' and '完全不亮' in follow['draft']['description'] and '一直闪' not in follow['draft']['description'], 'follow-up keeps location and replaces superseded symptom')
    v.check(v.request('GET', '/api/work-orders/summary', requester) == before, 'advice and drafts have not created an order')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend-image', default='campus-repair-api:natural-answers-check')
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--safety-only', action='store_true')
    mode.add_argument('--date-scope', action='store_true')
    mode.add_argument('--draft-advice', action='store_true')
    parser.add_argument('--port', type=int, default=18095)
    args = parser.parse_args()
    v = Verification(args.port)
    config = json.loads(v.config)
    for service in ('api','worker','mcp'):
        config['services'][service]['image'] = args.backend_image
    config['services']['web']['image'] = 'campus-repair-web:local'
    v.config = json.dumps(config)
    evidence = dict(v.evidence, answers=[])
    target = Path('tmp/natural-answers')
    target.mkdir(exist_ok=True)
    file = target / (v.project+'.json')
    try:
        v.dc('up','-d','--no-build')
        for _ in range(120):
            try:
                v.request('GET','/health/ready')
                break
            except (RuntimeError,OSError):
                time.sleep(.5)
        else:
            raise RuntimeError('Isolated instance failed readiness')
        for service in ('api','worker','mcp'):
            cid = v.dc('ps','-q',service).strip()
            runtime = json.loads(run(['docker','inspect',cid]))[0]
            assert runtime['Config']['Labels']['com.docker.compose.project']==v.project
            assert all(m['Type']=='volume' and m['Name'].startswith(v.project+'_') for m in runtime['Mounts'])
        tokens = {}
        for name in (('admin', 'tech') if args.safety_only else ('admin','requester')):
            tokens[name] = v.request('POST','/api/login',body={'username':name,'password':v.passwords['SEED_'+name.upper()+'_PASSWORD']})['access_token']
        admin = tokens['admin']
        agents = {}
        tools = v.request('GET','/api/mcp/tools',admin)['items']
        for a in v.request('GET','/api/agents?manage=1',admin)['items']:
            a['tool_names'] = [t['name'] for t in tools if t['role'] in ('all',a['role'])]
            agents[a['role']] = v.request('PUT',f"/api/agents/{a['id']}",admin,a)['agent']['id']
        if args.safety_only:
            evidence['answers'].append(verify_safety(v, admin, tokens['tech'], agents['technician']))
        elif args.draft_advice:
            verify_draft_advice(v, admin, tokens['requester'], agents['requester'], evidence['answers'])
        else:
            location = '虚构云杉楼'
            orders = [v.request('POST','/api/work-orders',tokens['requester'],{'location':location,'category':'lighting','description':'灯具频繁闪烁，暂无维修记录'},201,{'Idempotency-Key':f'natural-fictional-fixture-{i}'})['order'] for i in range(2)]
            for order in orders:
                v.dc('exec','-T','db','sh','-c','MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -e "$1"','sh',f"UPDATE work_orders SET created_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 14 DAY) WHERE id={int(order['id'])}")
            cid = None
            cases = [
                ('admin',location+'总结当前有哪些问题',None),
                ('admin','具体哪几单？',True),
                ('admin','为什么优先？你的依据是什么？',True),
                ('requester',orders[0]['number']+'为什么还没修好？',None),
                ('admin',location+'本周新增比上周多吗？',None),
            ]
            if args.date_scope:
                v.request('POST', '/api/work-orders', tokens['requester'],
                          {'location': location, 'category': 'lighting', 'description': '虚构本周新增'},
                          201, {'Idempotency-Key': 'natural-fictional-current-week'})
                cases = [
                    ('requester', '我的报修还有多少单未结束？', False),
                    ('requester', '那我的报修本周新增有几单？', True),
                    ('requester', '本周新增比上周多吗？', True),
                ]
            failures = []
            for role,question,follow in cases:
                started=time.perf_counter()
                cid,meta,answer=v.turn(agents[role],tokens[role],question,cid if follow else None)
                business=answer.get('business') or {}
                evidence['answers'].append({'question':question,'request_id':meta['request_id'],'duration_ms':round((time.perf_counter()-started)*1000),'answer':answer['answer'],'business':business})
                direct_count = args.date_scope and question == '我的报修还有多少单未结束？'
                if direct_count and (business.get('natural') or business.get('query', {}).get('queue') != 'active'):
                    failures.append('requester count did not use the direct active query')
                if not direct_count and business.get('natural') is not True:
                    failures.append('natural body missing: '+question)
                if '总结' in question or '哪几单' in question or '依据' in question:
                    if business.get('total')!=2 or business.get('query', {}).get('location')!=location:
                        failures.append('scope changed: '+question)
                if '比上周' in question and not business.get('additional'):
                    failures.append('comparison did not read a second window')
            if args.date_scope:
                current, weekly, comparison = evidence['answers']
                for row in evidence['answers']:
                    if any(phrase in row['answer'] for phrase in ('登录人授权范围', '工单日期不限')):
                        failures.append('internal scope label leaked into prose')
                if current['business'].get('total') != 3 or '未结束' not in current['answer'] and '没处理完' not in current['answer'] and '未结' not in current['answer']:
                    failures.append('current backlog count is incorrect or missing')
                if weekly['business'].get('total') != 1 or weekly['business'].get('query', {}).get('period') != 'this_week':
                    failures.append('this week must count only new work orders')
                prior = comparison['business'].get('additional', [])
                if comparison['business'].get('total') != 1 or not prior or prior[0].get('total') != 0:
                    failures.append('comparison must use nonzero this-week and zero previous-week counts')
                if not all(w in comparison['answer'] for w in ('本周', '上周')):
                    failures.append('comparison did not state both periods')
            if failures:
                raise RuntimeError(' / '.join(failures))
        evidence['result']='passed'
    except Exception as exc:
        evidence['result']='failed'
        evidence['failure']=str(exc) if isinstance(exc,(RuntimeError,AssertionError)) else type(exc).__name__
        print('FAIL '+evidence['failure'],flush=True)
    finally:
        try:
            (target/(v.project+'.log')).write_text(v.dc('logs','--tail','45','api','mcp'))
        finally:
            v.dc('down')
        file.write_text(json.dumps(evidence,ensure_ascii=False,indent=2))
        file.chmod(0o600)
        print('Evidence '+file.name,flush=True)
    if evidence['result']!='passed':
        raise SystemExit(1)

if __name__=='__main__':
    os.umask(0o077)
    main()
