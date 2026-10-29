#!/usr/bin/env python3
"""Encrypted local backup. Restore test always creates its own disposable DB."""
import argparse,datetime,hashlib,io,json,os,secrets,stat,subprocess,tarfile,time
from pathlib import Path
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
ROOT=Path(__file__).resolve().parents[2]
_rootless=Path(f'/run/user/{os.getuid()}/docker.sock')
_host=os.environ.get('GMA_DOCKER_HOST') or (str('unix://'+str(_rootless)) if _rootless.exists() and not os.environ.get('DOCKER_HOST') else '')
DOCKER=['docker']+(['--host',_host] if _host else [])
DB_IMAGE='postgres:18.6-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
MAX_SECRET=1024*1024
MAX_ARCHIVE=512*1024*1024
CORE=('config.json','encryption.key','database.password','instance.id')
RUNTIME_FILES={'secret_file':'encryption.key','smtp_file':'smtp.json','report_public_key_file':'report-public-key.json','ldap_file':'ldap.json','updater_token_file':'updater.token'}
OPTIONAL=('updater.token','bootstrap.token','smtp.json','smtp-ca.pem','report-public-key.json','ldap.json','ldap-ca.pem','migration-config.json')
def run(args,data=None):
 p=subprocess.run(DOCKER+args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
 if p.returncode:raise RuntimeError('Docker backup operation failed: '+args[0])
 return p.stdout

def read_secret(path):
 # Reject symlinks and bound reads. No arbitrary config-referenced path is followed.
 fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
 with os.fdopen(fd,'rb') as f:
  if not stat.S_ISREG(os.fstat(f.fileno()).st_mode):raise ValueError('secret must be a regular file')
  data=f.read(MAX_SECRET+1)
 if len(data)>MAX_SECRET:raise ValueError('secret exceeds limit')
 return data

def referenced_name(value,name,config):
 if not isinstance(value,str) or value not in ('/run/secrets/'+name,str(config/name)):
  raise ValueError('unsupported runtime dependency path')
 return name

def collect_secrets(config):
 config=Path(config).absolute()
 values={'secrets/'+n:read_secret(config/n) for n in CORE}
 if (config/'migration-config.json').exists():values['secrets/migration-config.json']=read_secret(config/'migration-config.json')
 runtime=json.loads(values['secrets/config.json'])
 if not isinstance(runtime,dict):raise ValueError('invalid runtime config')
 for field,name in RUNTIME_FILES.items():
  value=runtime.get(field,'')
  if field == 'secret_file' or value:
   referenced_name(value,name,config)
   if 'secrets/'+name not in values:values['secrets/'+name]=read_secret(config/name)
 if runtime.get('smtp_file'):
  smtp=json.loads(values['secrets/smtp.json'])
  if not isinstance(smtp,dict):raise ValueError('invalid SMTP config')
  if smtp.get('ca_file'):
   referenced_name(smtp['ca_file'],'smtp-ca.pem',config)
   values['secrets/smtp-ca.pem']=read_secret(config/'smtp-ca.pem')
 if runtime.get('ldap_file'):
  ldap=json.loads(values['secrets/ldap.json'])
  if not isinstance(ldap,dict):raise ValueError('invalid LDAP config')
  if ldap.get('ca_file'):
   referenced_name(ldap['ca_file'],'ldap-ca.pem',config)
   values['secrets/ldap-ca.pem']=read_secret(config/'ldap-ca.pem')
 return values

def validate_members(values,meta):
 allowed={'database.dump','manifest.json'}|{'secrets/'+n for n in CORE+OPTIONAL}
 if not set(values)<=allowed or not {'database.dump','manifest.json'}|{'secrets/'+n for n in CORE}<=set(values):raise ValueError('unexpected or missing archive members')
 if len(values['secrets/encryption.key'])!=32:raise ValueError('invalid application key length')
 if hashlib.sha256(values['database.dump']).hexdigest()!=meta['dump_sha256']:raise ValueError('database checksum mismatch')
 if meta.get('format')==1:return False
 if meta.get('format')!=2:raise ValueError('unsupported backup manifest')
 expected={n:{'size':len(v),'sha256':hashlib.sha256(v).hexdigest()} for n,v in values.items() if n!='manifest.json'}
 if meta.get('files')!=expected:raise ValueError('manifest does not match archive')
 runtime=json.loads(values['secrets/config.json'])
 for field,name in RUNTIME_FILES.items():
  value=runtime.get(field,'')
  if field == 'secret_file' or value:
   # The collector already resolved host paths. Archive only needs a fixed basename.
   if not isinstance(value,str) or Path(value).name!=name or 'secrets/'+name not in values:raise ValueError('missing runtime dependency')
 if runtime.get('smtp_file'):
  smtp=json.loads(values['secrets/smtp.json'])
  if smtp.get('ca_file') and ('secrets/smtp-ca.pem' not in values or Path(smtp['ca_file']).name!='smtp-ca.pem'):raise ValueError('missing SMTP CA')
 if runtime.get('ldap_file'):
  ldap=json.loads(values['secrets/ldap.json'])
  if ldap.get('ca_file') and ('secrets/ldap-ca.pem' not in values or Path(ldap['ca_file']).name!='ldap-ca.pem'):raise ValueError('missing LDAP CA')
 return True

def read_archive(args,path):
 with Path(path).open('rb') as f:raw=f.read(MAX_ARCHIVE+1)
 if len(raw)>MAX_ARCHIVE or raw[:7]!=b'GMABK01':raise ValueError('invalid backup header/size')
 plain=AESGCM(key(Path(args.key))).decrypt(raw[7:19],raw[19:],b'GMA|Planner backup v1')
 values={}
 with tarfile.open(fileobj=io.BytesIO(plain),mode='r:') as tar:
  for member in tar:
   if not member.isfile() or member.name in values or member.size>MAX_ARCHIVE:raise ValueError('invalid archive member')
   if member.name!='database.dump' and member.size>MAX_SECRET:raise ValueError('oversized secret member')
   values[member.name]=tar.extractfile(member).read()
 meta=json.loads(values['manifest.json'])
 complete=validate_members(values,meta)
 return values,meta,complete

def counts(container, database="planner"):
 sql="SELECT 'projects',count(*) FROM projects UNION ALL SELECT 'tasks',count(*) FROM tasks UNION ALL SELECT 'dependencies',count(*) FROM dependencies UNION ALL SELECT 'users',count(*) FROM users UNION ALL SELECT 'time_entries',count(*) FROM time_entries UNION ALL SELECT 'calendars',count(*) FROM calendars ORDER BY 1;"
 return run(['exec',container,'psql','-U','planner','-d',database,'-At','-c',sql]).decode().strip()
def key(path,create=False):
 if create and not path.exists():
  path.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
  fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
  with os.fdopen(fd,'wb') as f:f.write(secrets.token_bytes(32))
 if path.stat().st_mode&0o077:raise RuntimeError('backup key must be mode 0600')
 return path.read_bytes()
def prune(directory):
 # Keep newest archive for each of 7 days, 4 ISO weeks and 3 months.
 files=sorted(directory.glob('backup-*.gma'),reverse=True);keep=set();buckets=[set(),set(),set()]
 for f in files:
  stamp=datetime.datetime.strptime(f.name[7:22],'%Y%m%dT%H%M%S');iso=stamp.isocalendar();labels=[stamp.date().isoformat(),f'{iso.year}-{iso.week}',stamp.strftime('%Y-%m')]
  for i,limit in enumerate([7,4,3]):
   if labels[i] not in buckets[i] and len(buckets[i])<limit:keep.add(f);buckets[i].add(labels[i])
 for f in files:
  if f not in keep:f.unlink()
def backup(args):
 config=Path(args.config);dest=Path(args.destination)
 values=collect_secrets(config)
 dest.mkdir(parents=True,exist_ok=True,mode=0o700);k=key(Path(args.key),True)
 dump=run(['exec',args.container,'pg_dump','-U','planner','-d','planner','-Fc','--no-owner','--no-acl'])
 if len(dump)>MAX_ARCHIVE-MAX_SECRET*16:raise ValueError('database dump exceeds limit')
 values['database.dump']=dump
 meta={'created_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'dump_sha256':hashlib.sha256(dump).hexdigest(),'counts':counts(args.container),'format':2,'files':{n:{'size':len(v),'sha256':hashlib.sha256(v).hexdigest()} for n,v in values.items()}}
 values['manifest.json']=json.dumps(meta).encode()
 validate_members(values,meta)
 buf=io.BytesIO()
 with tarfile.open(fileobj=buf,mode='w') as tar:
  for name,data in values.items():
   item=tarfile.TarInfo(name);item.size=len(data);item.mode=0o600;tar.addfile(item,io.BytesIO(data))
 nonce=secrets.token_bytes(12);data=b'GMABK01'+nonce+AESGCM(k).encrypt(nonce,buf.getvalue(),b'GMA|Planner backup v1')
 path=dest/('backup-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S')+'.gma');fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
 with os.fdopen(fd,'wb') as f:f.write(data)
 prune(dest);print(json.dumps({'backup':str(path),'encrypted_bytes':len(data),'manifest_format':2}))
 return path

def runtime_smoke(args,name,values):
 binary=Path(getattr(args,'planner_bin',str(ROOT/'production/bin/planner')))
 if not binary.is_file():raise ValueError('planner binary required for runtime restore verification')
 config=json.loads(values['secrets/config.json'])
 # Disposable role has no elevated privileges. No production DSN is contacted.
 role='restore_runtime'
 sql="REVOKE CREATE ON SCHEMA public FROM PUBLIC; CREATE ROLE restore_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION; GRANT CONNECT ON DATABASE planner TO restore_runtime; GRANT USAGE ON SCHEMA public TO restore_runtime; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO restore_runtime; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO restore_runtime;"
 run(['exec',name,'psql','-U','planner','-d','planner','-v','ON_ERROR_STOP=1','-c',sql])
 config['database_url']='postgres://'+role+'@127.0.0.1:5432/planner?sslmode=disable'
 config['web_dir']='/tmp/gma-restore/web'
 config.pop('updater_socket',None);config.pop('updater_token_file',None)
 for field,filename in RUNTIME_FILES.items():
  if config.get(field):config[field]='/tmp/gma-restore/'+filename
 copied={n.removeprefix('secrets/'):v for n,v in values.items() if n.startswith('secrets/')}
 copied['config.json']=json.dumps(config).encode()
 if config.get('ldap_file'):
  ldap=json.loads(copied['ldap.json'])
  if ldap.get('ca_file'):ldap['ca_file']='/tmp/gma-restore/ldap-ca.pem'
  copied['ldap.json']=json.dumps(ldap).encode()
 if config.get('smtp_file'):
  smtp=json.loads(copied['smtp.json'])
  if smtp.get('ca_file'):smtp['ca_file']='/tmp/gma-restore/smtp-ca.pem'
  copied['smtp.json']=json.dumps(smtp).encode()
 buf=io.BytesIO()
 with tarfile.open(fileobj=buf,mode='w') as tar:
  for filename,data in copied.items():
   item=tarfile.TarInfo(filename);item.size=len(data);item.mode=0o600;item.uid=65532;item.gid=65532;tar.addfile(item,io.BytesIO(data))
 run(['exec','--user','0:0',name,'mkdir','-p','/tmp/gma-restore/web'])
 run(['exec','-i','--user','0:0',name,'tar','-xf','-','-C','/tmp/gma-restore'],buf.getvalue())
 run(['cp',str(binary),name+':/tmp/gma-restore/planner'])
 run(['exec','--user','0:0',name,'chmod','755','/tmp/gma-restore/planner'])
 run(['exec','-d','--user','65532:65532',name,'/tmp/gma-restore/planner','api','--config','/tmp/gma-restore/config.json','--listen','127.0.0.1:18080'])
 for _ in range(30):
  try:
   response=run(['exec',name,'wget','-qO-','http://127.0.0.1:18080/readyz'])
   if json.loads(response).get('ready') is True:return
  except (RuntimeError,ValueError):pass
  time.sleep(.2)
 raise RuntimeError('restored API readiness failed')

def verify(args,path):
 values,meta,complete=read_archive(args,path)
 dump=values['database.dump']
 name='gma-restore-check-'+secrets.token_hex(6);started=time.monotonic()
 try:
  # No published port/network; trust is confined to a fresh disposable container.
  run(['run','-d','--name',name,'--network','none','--tmpfs','/var/lib/postgresql','-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_USER=planner','-e','POSTGRES_DB=planner',DB_IMAGE])
  for _ in range(30):
   try:run(['exec',name,'pg_isready','-h','127.0.0.1','-U','planner']);break
   except RuntimeError:time.sleep(1)
  run(['exec','-i',name,'pg_restore','-U','planner','-d','planner','--no-owner','--no-acl','--exit-on-error'],dump)
  assert counts(name)==meta['counts'],'Restored row counts differ'
  if complete:runtime_smoke(args,name,values)
  print(json.dumps({'restore_verified':True,'runtime_verified':complete,'legacy_database_only':not complete,'seconds':round(time.monotonic()-started,2),'counts':meta['counts'].splitlines()}))
 finally:
  run(['rm','-f',name])
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--config',default=str(Path.home()/'.config/gm-vector'));p.add_argument('--key',default=str(Path.home()/'.config/gm-vector-backup/key'));p.add_argument('--destination',default=str(Path.home()/'.local/share/gm-vector/backups'));p.add_argument('--container',required=True,help='Explicit PostgreSQL container to back up; never inferred');p.add_argument('--planner-bin',default=str(ROOT/'production/bin/planner'));p.add_argument('--verify',action='store_true');p.add_argument('--verify-file');a=p.parse_args()
 if a.verify_file:verify(a,a.verify_file)
 else:
  path=backup(a)
  if a.verify:verify(a,path)
