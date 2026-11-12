"""Read-only deployment guard; never prints environment/configuration values."""
import json, subprocess

def run(args):
    r=subprocess.run(args,capture_output=True,text=True)
    if r.returncode:raise SystemExit('Deployment inspection failed; raw configuration suppressed')
    return r.stdout

def verify(configured, running, target):
    if len(set(configured.values()))!=1:raise ValueError('API/worker/MCP configured images differ')
    if set(running)!=set(configured):raise ValueError('Backend container missing')
    if any(value!=target for value in running.values()):raise ValueError('Backend container uses an old or inconsistent image; update api worker mcp together')

if __name__=='__main__':
    services=('api','worker','mcp')
    config=json.loads(run(['docker','compose','config','--format','json']))
    configured={s:config['services'][s]['image'] for s in services}
    target=json.loads(run(['docker','image','inspect',configured['api']]))[0]['Id']
    running={}
    for s in services:
        cid=run(['docker','compose','ps','-q',s]).strip()
        if not cid:raise SystemExit('Backend container missing: '+s)
        running[s]=json.loads(run(['docker','inspect',cid]))[0]['Image']
    try:verify(configured,running,target)
    except ValueError as e:raise SystemExit(str(e))
    webid=run(['docker','compose','ps','-q','web']).strip()
    if not webid:raise SystemExit('Web container missing')
    web=json.loads(run(['docker','inspect',webid]))[0]
    webtarget=json.loads(run(['docker','image','inspect',config['services']['web']['image']]))[0]['Id']
    if web['Image']!=webtarget:raise SystemExit('Web container uses an old image; update web together with backend')
    print('PASS API/worker/MCP use the current backend image; web uses its current image')
