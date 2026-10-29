#!/usr/bin/env python3
"""Build a portable GM Vector offline archive directory from locally built images."""
import argparse,hashlib,importlib.util,json,os,shutil,subprocess
from pathlib import Path
HERE=Path(__file__).resolve().parent
PRODUCTION=HERE.parent
spec=importlib.util.spec_from_file_location('installer',HERE/'install.py');installer=importlib.util.module_from_spec(spec);spec.loader.exec_module(installer)
def regular_source(path,tree=False):
 path=Path(path).absolute()
 if any(p.is_symlink() for p in [path,*path.parents]):raise ValueError('symlink source forbidden')
 if tree:
  if not path.is_dir():raise ValueError('directory required')
  for child in path.iterdir():regular_source(child,child.is_dir())
 elif not path.is_file():raise ValueError('regular source required')
def main():
 p=argparse.ArgumentParser(description=__doc__)
 for name in ('app','db','proxy'):p.add_argument('--'+name+'-image',required=True)
 p.add_argument('--out',type=Path,required=True)
 p.add_argument('--report-public-key',type=Path)
 a=p.parse_args();out=a.out.expanduser().resolve()
 if out==PRODUCTION.parent or PRODUCTION.parent in out.parents:p.error('output must be outside source repository')
 key=None
 if a.report_public_key:regular_source(a.report_public_key);key=installer.report_public_key(a.report_public_key)
 out.mkdir(parents=True,mode=0o700,exist_ok=False)
 images={}
 for name in ('app','db','proxy'):
  ref=getattr(a,name+'_image')
  digest=subprocess.check_output(installer.docker()+['image','inspect','--format','{{.Id}}',ref],text=True).strip()
  if not digest.startswith('sha256:') or len(digest)!=71:raise ValueError('invalid image identity')
  images[name]=digest
 for name in ('install.py','compose.yaml','README.md'):
  regular_source(HERE/name);shutil.copyfile(HERE/name,out/name)
 for name in ('compose.https.yaml','compose.https.files.yaml'):
  regular_source(PRODUCTION/name);shutil.copyfile(PRODUCTION/name,out/name)
 (out/'tls').mkdir()
 for name in ('Caddyfile.files','Caddyfile.acme'):
  source=PRODUCTION/'tls'/name;regular_source(source);shutil.copyfile(source,out/'tls'/name)
 for name in ('database-roles.py','recover-admin.sh'):
  source=PRODUCTION/'scripts'/name
  if source.exists():regular_source(source);shutil.copyfile(source,out/name)
 for name in ('LICENSE','NOTICE'):
  source=PRODUCTION.parent/name
  if not source.exists():source=PRODUCTION.parent/'community'/name
  regular_source(source);shutil.copyfile(source,out/name)
 docs=PRODUCTION.parent/'community/docs'
 if not docs.exists():docs=PRODUCTION.parent/'docs'
 (out/'docs').mkdir()
 for name in ('installation.md','https.md','recovery.md','api.md','directory.md','integrations.md','localization.md'):
  source=docs/name;regular_source(source);shutil.copyfile(source,out/'docs'/name)
 (out/'docs/contracts').mkdir()
 source=docs/'contracts/openapi.json';regular_source(source);shutil.copyfile(source,out/'docs/contracts/openapi.json')
 if key is not None:(out/'report-public-key.json').write_text(json.dumps(key,sort_keys=True))
 if (PRODUCTION/'notices').is_dir():
  regular_source(PRODUCTION/'notices',True);shutil.copytree(PRODUCTION/'notices',out/'notices')
 subprocess.run(installer.docker()+['save','--output',str(out/'images.tar'),*images.values()],check=True)
 files={}
 for path in sorted(out.rglob('*')):
  regular_source(path,path.is_dir())
  if path.is_file():
   h=hashlib.sha256()
   with path.open('rb') as f:
    for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
   files[path.relative_to(out).as_posix()]=h.hexdigest()
 (out/'package.json').write_text(json.dumps({'format':'gm-vector-package-v1','images':images,'files':files},indent=2))
 print('Package prepared. Verify and sign the official distribution separately; checksums alone are not publisher authentication.')
if __name__=='__main__':main()
