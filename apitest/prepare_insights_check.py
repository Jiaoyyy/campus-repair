"""Prepare only the dedicated fictional analysis-check environment; preserve its data."""
import argparse
import json
from pathlib import Path
import secrets
import subprocess

ROOT=Path(__file__).resolve().parents[1]
PROJECT='campus-repair-insights-check'
FOLDER=ROOT/'tmp/insights'

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('--backend-image',default='campus-repair-api:latest')
 parser.add_argument('--web-image',default='campus-repair-web:latest')
 args=parser.parse_args()
 FOLDER.mkdir(parents=True,exist_ok=True)
 envpath=FOLDER/'check.env'
 if not envpath.exists():
  env={'DB_PASSWORD':secrets.token_hex(20),'DB_ROOT_PASSWORD':secrets.token_hex(20),'SESSION_SECRET':secrets.token_hex(32),'MCP_CALL_SECRET':secrets.token_hex(32),'WEB_PORT':'18088'}
  for name in ('ADMIN','REQUESTER','REQUESTER2','TECH','TECH2','OTHER'):env['SEED_'+name+'_PASSWORD']=secrets.token_hex(12)
  envpath.write_text(''.join(k+'='+v+'\n' for k,v in env.items()));envpath.chmod(0o600)
 state=FOLDER/'mock';state.mkdir(exist_ok=True)
 if not (state/'chat-mode').exists():(state/'chat-mode').write_text('insight')
 configpath=FOLDER/'compose.json'
 if configpath.exists():
  config=json.loads(configpath.read_text())
 else:
  # Supply the project BEFORE config expansion: resolved volumes/networks are named here.
  command=['docker','compose','-p',PROJECT,'--env-file',str(envpath),'-f','compose.yaml','-f','compose.verify.yaml','-f','compose.knowledge-check.yaml','config','--format','json']
  result=subprocess.run(command,cwd=ROOT,capture_output=True,text=True)
  if result.returncode:raise RuntimeError('Could not resolve isolated compose configuration; private diagnostics suppressed')
  config=json.loads(result.stdout)
  for service in ('chat-mock','embedding-mock'):
   for volume in config['services'][service]['volumes']:
    if volume['target']=='/state':volume['source']=str(state)
 if config.get('name')!=PROJECT:raise RuntimeError('Refusing unexpected project')
 for kind in ('volumes','networks'):
  if any(not item.get('name','').startswith(PROJECT+'_') for item in config.get(kind,{}).values()):raise RuntimeError('Refusing shared volume/network names')
 ports=config['services']['web'].get('ports',[])
 if len(ports)!=1 or str(ports[0].get('published'))!='18088' or ports[0].get('host_ip')!='127.0.0.1':raise RuntimeError('Refusing unexpected exposed port')
 if config['services']['api']['environment'].get('CHAT_BASE_URL')!='http://chat-mock:8080/v1':raise RuntimeError('Refusing non-mock configuration')
 for service in ('api','worker','mcp'):
  config['services'][service]['image']=args.backend_image
  config['services'][service].pop('build',None)
 config['services']['web']['image']=args.web_image;config['services']['web'].pop('build',None)
 configpath.write_text(json.dumps(config,indent=2));configpath.chmod(0o600)
 command=['docker','compose','-p',PROJECT,'--project-directory',str(ROOT),'-f',str(configpath)]
 result=subprocess.run(command+['up','-d','--no-build'],cwd=ROOT,capture_output=True,text=True)
 if result.returncode:raise RuntimeError('Isolated startup failed; private diagnostics suppressed')
 # Mounted Python sources are loaded at process start, so refresh mocks after code updates.
 result=subprocess.run(command+['restart','chat-mock','embedding-mock'],cwd=ROOT,capture_output=True,text=True)
 if result.returncode:raise RuntimeError('Could not refresh isolated mocks')
 for service in ('db','qdrant'):
  runtime=json.loads(subprocess.check_output(['docker','inspect',PROJECT+'-'+service+'-1']))[0]
  if any(not mount.get('Name','').startswith(PROJECT+'_') for mount in runtime['Mounts'] if mount['Type']=='volume'):raise RuntimeError('Runtime volume is not isolated')
 print('Isolated mock environment ready: http://127.0.0.1:18088; existing data retained.')

if __name__=='__main__':main()
