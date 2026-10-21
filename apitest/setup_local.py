"""Create local secrets without printing them or overwriting existing configuration."""
from pathlib import Path
import secrets,os
root=Path(__file__).resolve().parents[1]
target=root/'.env'
if target.exists(): raise SystemExit('.env exists; left unchanged')
text=(root/'.env.example').read_text()
lines=[]
for line in text.splitlines():
 if '=' in line and not line.startswith('#'):
  key,value=line.split('=',1)
  if not value and (key in ('DB_PASSWORD','DB_ROOT_PASSWORD','SESSION_SECRET','MCP_CALL_SECRET') or key.startswith('SEED_')):
   line=key+'='+secrets.token_hex(32 if key in ('SESSION_SECRET','MCP_CALL_SECRET') else 24)
 lines.append(line)
fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f:f.write('\n'.join(lines)+'\n')
print('Created local .env. Credentials were not printed; open this local file to sign in.')
