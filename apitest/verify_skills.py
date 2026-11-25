"""Skill configuration regression against the dedicated local mock environment.
Requires tmp/tools-skills-check/compose.json and private credentials.json.
Never targets daily or manual-review data. Does not print credentials.
"""
import concurrent.futures
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
HOME = ROOT / 'tmp/tools-skills-check'
PROJECT = 'campus-repair-tools-skills-check'
BASE = 'http://127.0.0.1:18090'
DC = ['docker', 'compose', '-p', PROJECT, '--project-directory', str(ROOT), '-f', str(HOME / 'compose.json')]
checks = 0


def require(condition, description):
    global checks
    if not condition:
        raise AssertionError(description)
    checks += 1
    print('PASS ' + description, flush=True)


def dc(*args, data=None):
    result = subprocess.run(DC + list(args), input=data, capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError('Isolated operation failed; raw output suppressed')
    return result.stdout.strip()


def sql(statement):
    return dc('exec', '-T', 'db', 'sh', '-c', 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --default-character-set=utf8mb4 -u root "$MYSQL_DATABASE" -N -B', data=statement)


def request(method, path, token=None, body=None, expected=200):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    data = json.dumps(body).encode() if body is not None else None
    try:
        with urllib.request.urlopen(urllib.request.Request(BASE + path, method=method, headers=headers, data=data), timeout=15) as response:
            status, result = response.status, json.load(response)
    except urllib.error.HTTPError as response:
        status, result = response.code, json.load(response)
    require(status == expected, method + ' ' + path + ' HTTP ' + str(expected))
    return result


def ready():
    for _ in range(50):
        try:
            with urllib.request.urlopen(BASE + '/health/ready', timeout=2) as response:
                if response.status == 200:
                    return
        except (OSError, ValueError):
            pass
        time.sleep(.2)
    raise RuntimeError('Isolated API did not recover')


for service in ('api', 'db', 'worker', 'mcp'):
    container = json.loads(subprocess.check_output(['docker', 'inspect', PROJECT + '-' + service + '-1']))[0]
    require(container['Config']['Labels']['com.docker.compose.project'] == PROJECT, service + ' isolation')
    require(all(m['Type'] == 'volume' and m['Name'].startswith(PROJECT + '_') for m in container['Mounts']), service + ' private volumes')
    if service == 'api':
        runtime = dict(value.split('=', 1) for value in container['Config']['Env'])
        require(runtime['CHAT_BASE_URL'] == 'http://chat-mock:8080/v1', 'actual mock dependency')

# Optional injected migration failure from the dedicated setup fixture.
if sql("SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND trigger_name='reject_skills_migration'") == '1':
    for _ in range(30):
        logs = dc('logs', '--tail', '10', 'api')
        if 'database initialization failed' in logs:
            break
        time.sleep(.2)
    require('database initialization failed' in logs, 'injected migration failure reached API startup')
    require(sql('SELECT COUNT(*) FROM schema_migrations WHERE version=9') == '0', 'failed migration marker rolled back')
    require(sql('SELECT COUNT(*) FROM agent_skills') == '0', 'failed seed bindings rolled back')
    require(sql('SELECT COUNT(*) FROM skills') == '0', 'failed skill catalog rolled back')
    baseline = json.loads((HOME / 'legacy-baseline.json').read_text())
    require(all(sql('CHECKSUM TABLE ' + table) == digest for table, digest in baseline.items()), 'legacy tables preserved after failure')
    sql('DROP TRIGGER reject_skills_migration')
    dc('restart', 'api')
    ready()
    require(sql('SELECT COUNT(*) FROM schema_migrations WHERE version=9') == '1', 'retry migration completed once')
    require(all(sql('CHECKSUM TABLE ' + table) == digest for table, digest in baseline.items()), 'legacy tables preserved after recovery')

ready()
credentials = json.loads((HOME / 'credentials.json').read_text())
tokens = {}
for user, key in (('admin', 'ADMIN'), ('requester', 'REQUESTER'), ('other_admin', 'OTHER')):
    tokens[user] = request('POST', '/api/login', body={'username': user, 'password': credentials['SEED_' + key + '_PASSWORD']})['access_token']
admin, requester, other_admin = tokens['admin'], tokens['requester'], tokens['other_admin']
catalog = request('GET', '/api/skills', admin)['items']
require({item['id'] for item in catalog} == {'repair_intake', 'safe_self_check', 'dispatch_advice'}, 'three complete builtin definitions')
require(all(item['definition_version'] >= 1 and item['instruction'] and item['output_contract'] for item in catalog), 'versioned skill contracts')
request('GET', '/api/skills', requester, expected=403)
request('PUT', '/api/skills/safe_self_check', requester, {'enabled': False, 'version': 1}, expected=403)
agents = request('GET', '/api/agents?manage=1', admin)['items']
requester_agent = next(a for a in agents if a['role'] == 'requester')
require({x['skill_id'] for x in requester_agent['skill_bindings']} == {'repair_intake', 'safe_self_check'}, 'one-time requester seed bindings')
request('PUT', '/api/agents/' + str(requester_agent['id']), other_admin, requester_agent, expected=404)
request('PUT', '/api/agents/' + str(requester_agent['id']), requester, requester_agent, expected=403)
invalid = {**requester_agent, 'skill_bindings': [{'skill_id': 'dispatch_advice', 'enabled': True}]}
request('PUT', '/api/agents/' + str(requester_agent['id']), admin, invalid, expected=400)
invalid = {**requester_agent, 'skill_bindings': [{'skill_id': 'unknown', 'enabled': True}]}
request('PUT', '/api/agents/' + str(requester_agent['id']), admin, invalid, expected=400)
invalid = {**requester_agent, 'skill_bindings': requester_agent['skill_bindings'] * 2}
request('PUT', '/api/agents/' + str(requester_agent['id']), admin, invalid, expected=400)
original = requester_agent
try:
    # Missing required tools are retained as visible configuration gaps.
    payload = {**original, 'tool_names': [], 'skill_bindings': [{'skill_id': 'safe_self_check', 'enabled': True}]}
    saved = request('PUT', '/api/agents/' + str(original['id']), admin, payload)['agent']
    require(saved['skill_bindings'] == payload['skill_bindings'], 'binding with missing tool saved for configuration recovery')
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        def concurrent_save(_):
            headers = {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + admin}
            req = urllib.request.Request(BASE + '/api/agents/' + str(saved['id']), method='PUT', headers=headers, data=json.dumps(saved).encode())
            try:
                with urllib.request.urlopen(req, timeout=10) as response:
                    return response.status
            except urllib.error.HTTPError as response:
                return response.code
        statuses = sorted(pool.map(concurrent_save, range(2)))
    require(statuses == [200, 409], 'concurrent agent saves preserve a single version winner')
    live = next(a for a in request('GET', '/api/agents?manage=1', admin)['items'] if a['id'] == original['id'])
    omitted = {key: value for key, value in live.items() if key != 'skill_bindings'}
    live = request('PUT', '/api/agents/' + str(live['id']), admin, omitted)['agent']
    require(live['skill_bindings'] == payload['skill_bindings'], 'old caller omission preserves bindings')
    cleared = request('PUT', '/api/agents/' + str(live['id']), admin, {**live, 'skill_bindings': []})['agent']
    require(cleared['skill_bindings'] == [], 'explicit empty binding list clears skills')
    before = {table: sql('CHECKSUM TABLE ' + table) for table in ('skills', 'agent_skills', 'repair_drafts', 'messages')}
    dc('restart', 'api')
    ready()
    require(all(sql('CHECKSUM TABLE ' + table) == digest for table, digest in before.items()), 'restart preserves removals, legacy drafts and messages')
    skill = next(s for s in catalog if s['id'] == 'safe_self_check')
    other_before = request('GET', '/api/skills', other_admin)['items']
    changed = request('PUT', '/api/skills/safe_self_check', admin, {'version': skill['version'], 'enabled': False})['skill']
    request('PUT', '/api/skills/safe_self_check', admin, {'version': skill['version'], 'enabled': True}, expected=409)
    require(request('GET', '/api/skills', other_admin)['items'] == other_before, 'organization skill switch is isolated')
    request('PUT', '/api/skills/safe_self_check', admin, {'version': changed['version'], 'enabled': True})
finally:
    live = next(a for a in request('GET', '/api/agents?manage=1', admin)['items'] if a['id'] == original['id'])
    request('PUT', '/api/agents/' + str(live['id']), admin, {**original, 'version': live['version']})

print('PASS skill configuration checks ' + str(checks))
