#!/usr/bin/env python3
"""Build GM Vector from source. Publisher-signed metadata is optional provenance."""
import argparse, base64, datetime, json, os, shutil, subprocess, tempfile
from pathlib import Path
root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--publisher-keys', type=Path)
p.add_argument('--release', type=Path)
p.add_argument('--synthetic', action='store_true', help='generate ephemeral test provenance')
p.add_argument('--synthetic-key-dir', type=Path)
p.add_argument('--backend-only', action='store_true')
p.add_argument('--version', default='0.2.0-dev')
a=p.parse_args()
go=Path(shutil.which('go') or str(Path.home()/'.local/share/gma-toolchain/go/bin/go'))
env=os.environ.copy(); env['CGO_ENABLED']='0'
try: commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True,stderr=subprocess.DEVNULL).strip()
except subprocess.CalledProcessError: commit='unknown'
def run(command,**kw): return subprocess.run(command,cwd=root,env=env,check=True,**kw)
def release(*args,**kw): return run([str(go),'run','./cmd/release',*map(str,args)],**kw)
with tempfile.TemporaryDirectory(prefix='gm-vector-build-') as temp:
 temp=Path(temp)
 if a.synthetic:
  if a.publisher_keys or a.release: p.error('synthetic keys are generated independently')
  keydir=a.synthetic_key_dir.resolve() if a.synthetic_key_dir else temp/'issuer'
  if keydir==root or root in keydir.parents: p.error('test private keys must stay outside source/image')
  release('keygen','--dir',keydir,'--key-id','synthetic-ci')
  a.publisher_keys=keydir/'public-keys.json'; a.release=temp/'release.json'
  release('sign-release','--key',keydir/'signing.key','--key-id','synthetic-ci','--version','0.1.0-synthetic','--date',datetime.datetime.now(datetime.timezone.utc).date().isoformat(),'--commit',commit,'--channel','synthetic','--out',a.release)
 elif a.synthetic_key_dir: p.error('--synthetic-key-dir requires --synthetic')
 if bool(a.publisher_keys)!=bool(a.release): p.error('--publisher-keys and --release are required together')
 package='github.com/GM-Access/gm-vector/production/internal/planner.'
 values={'embeddedGitCommit':commit}
 if a.release:
  metadata=json.loads(release('verify-release','--keys',a.publisher_keys,'--file',a.release,*(['--allow-synthetic'] if a.synthetic else []),capture_output=True,text=True).stdout)
  if metadata['git_commit']!=commit: p.error('signed release does not match source commit')
  if not a.synthetic and subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip(): p.error('official signed build requires clean committed sources')
  encode=lambda b:base64.b64encode(b).decode().rstrip('=')
  values.update(embeddedPublisherKeys=encode(a.publisher_keys.read_bytes()),embeddedRelease=encode(a.release.read_bytes()),embeddedBuildMode=metadata['channel'])
  releasebytes=a.release.read_bytes(); keybytes=a.publisher_keys.read_bytes()
 else:
  import re
  if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?',a.version): p.error('invalid version')
  date=datetime.datetime.now(datetime.timezone.utc).date().isoformat()
  metadata={'format':'gm-vector-release-v1','version':a.version,'release_date':date,'git_commit':commit,'channel':'community'}
  values.update(embeddedBuildMode='community',communityVersion=a.version,communityDate=date)
  releasebytes=json.dumps(metadata).encode(); keybytes=b'{}'
 flags='-s -w '+' '.join('-X '+package+k+'='+v for k,v in values.items())
 (root/'bin').mkdir(exist_ok=True)
 run([str(go),'build','-buildvcs=false','-trimpath','-ldflags='+flags,'-o','bin/planner','./cmd/planner'])
 run([str(go),'build','-buildvcs=false','-trimpath','-ldflags=-s -w','-o','bin/release-verifier','./cmd/release'])
 (root/'bin/release.json').write_bytes(releasebytes)
 (root/'bin/publisher-keys.json').write_bytes(keybytes)
if not a.backend_only:
 subprocess.run(['npm','ci','--ignore-scripts'],cwd=root/'web',check=True)
 subprocess.run(['npm','run','build'],cwd=root/'web',check=True)
 subprocess.run(['python3',str(root/'scripts/openapi.py')],check=True)
 subprocess.run([str(go),'build','-buildvcs=false','-trimpath','-ldflags=-s -w','-o','../bin/caddy','.'],cwd=root/'proxy',env=env,check=True)
 subprocess.run(['python3',str(root/'scripts/notices.py')],check=True)
print('Built',metadata['version'],metadata['git_commit'],metadata['channel'])
