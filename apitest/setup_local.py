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
  if not value: line=key+'='+secrets.token_hex(24 if key!='SESSION_SECRET' else 32)
 lines.append(line)
fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f:f.write('\n'.join(lines)+'\n')
print('Created local .env. Credentials were not printed; open this local file to sign in.')
