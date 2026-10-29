#!/usr/bin/env python3
"""Collect upstream notices from installed, pinned build dependencies."""
import json,subprocess,shutil
from pathlib import Path
root=Path(__file__).resolve().parents[1];go=shutil.which('go') or str(Path.home()/'.local/share/gma-toolchain/go/bin/go');out=root/'notices';out.mkdir(exist_ok=True);records=[];texts=[]
def record(name,version,directory):
 files=[];d=Path(directory)
 for p in sorted(d.iterdir()):
  if p.is_file() and p.name.upper().startswith(('LICENSE','LICENCE','COPYING','NOTICE','COPYRIGHT')):
   text=p.read_text(errors='replace');files.append(p.name);texts.append('\n'+'='*72+'\n'+name+' @ '+version+' / '+p.name+'\n'+'='*72+'\n'+text)
 records.append({'name':name,'version':version,'notice_files':files})
seen=set()
for cwd in [root,root/'proxy']:
 raw=subprocess.check_output([go,'list','-m','-json','all'],cwd=cwd).decode();decoder=json.JSONDecoder()
 while raw.strip():
  value,n=decoder.raw_decode(raw.lstrip());raw=raw.lstrip()[n:]
  identity=(value['Path'],value.get('Version',''))
  if value.get('Main') or identity in seen or not value.get('Dir'):continue
  seen.add(identity);record(*identity,value['Dir'])
lock=json.loads((root/'web/package-lock.json').read_text())
for name,v in lock['packages'].items():
 d=root/'web'/name
 if name and d.is_dir():record(v.get('name') or name.split('node_modules/')[-1],v.get('version',''),d)
(out/'THIRD-PARTY-NOTICES.txt').write_text('GM Vector — upstream notices. Third-party components retain their own licenses.\n'+''.join(texts))
(out/'inventory.json').write_text(json.dumps(records,indent=2)+'\n')
missing=[r['name'] for r in records if not r['notice_files']];print(json.dumps({'components':len(records),'missing_root_notice':missing}))
