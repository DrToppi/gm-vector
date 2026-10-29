#!/usr/bin/env python3
"""Explicitly configure the optional host updater. Does not start or enable it."""
import argparse, json, os, secrets, subprocess
from pathlib import Path
from update_engine import atomic, private_tree

p=argparse.ArgumentParser(description=__doc__);p.add_argument('--state',type=Path,required=True)
p.add_argument('--source',type=Path,default=Path(__file__).resolve().parents[2]);p.add_argument('--unit-dir',type=Path);a=p.parse_args()
state=private_tree(a.state);source=a.source.resolve()
if any(c in str(source)+str(state) for c in '\n\r%'): p.error('unsupported source/state path')
if subprocess.check_output(['git','status','--porcelain'],cwd=source,text=True).strip(): p.error('commit sources before configuring updater')
commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=source,text=True).strip()
deployment=json.loads((state/'deployment.json').read_text())
if deployment['commit']!=commit:p.error('deploy this commit before configuring its host service')
root=state/'updater';root.mkdir(mode=0o700,exist_ok=True);private_tree(root)
shared=root/'shared';shared.mkdir(mode=0o755,exist_ok=True)
if shared.is_symlink() or shared.stat().st_uid != os.getuid():p.error('unsafe shared directory')
shared.chmod(0o755)
tokenfile=state/'config/updater.token'
if tokenfile.is_symlink():p.error('unsafe credential file')
if not tokenfile.exists():
 fd=os.open(tokenfile,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:f.write(secrets.token_urlsafe(48))
configpath=state/'config/config.json';config=json.loads(configpath.read_text())
config.update(updater_socket='/run/updater/agent.sock',updater_token_file='/run/secrets/updater.token');atomic(configpath,config)
# A data-only overlay, never a Docker socket mount.
overlay='services:\n  api:\n    volumes:\n      - '+json.dumps(str(shared)+':/run/updater:ro')+'\n'
fd=os.open(state/'updater.compose.yaml',os.O_WRONLY|os.O_CREAT|os.O_TRUNC|os.O_NOFOLLOW,0o600)
with os.fdopen(fd,'w') as f:f.write(overlay)
def quote(value):return '"'+str(value).replace('\\','\\\\').replace('"','\\"')+'"'
unit='''[Unit]
Description=gmVector host updater
After=default.target
[Service]
Type=simple
ExecStart=/usr/bin/python3 '''+quote(source/'production/deploy/update_service.py')+' serve --state '+quote(state)+' --source '+quote(source)+'''
Restart=on-failure
RestartSec=5
UMask=0077
KillMode=control-group
NoNewPrivileges=true
[Install]
WantedBy=default.target
'''
name='gm-vector-updater-'+json.loads((state/'installation.json').read_text())['project']+'.service'
folder=a.unit_dir or Path.home()/'.config/systemd/user';folder.mkdir(mode=0o700,parents=True,exist_ok=True)
path=folder/name
if path.is_symlink():p.error('unsafe unit file')
path.write_text(unit);path.chmod(0o600)
print('Configured optional updater. Re-run install.py prepare for this state.')
print('Then run: systemctl --user daemon-reload')
print('Then run: systemctl --user enable --now '+name)
print('Host source must remain available at its configured path; self-upgrade of the agent is an operator action.')
