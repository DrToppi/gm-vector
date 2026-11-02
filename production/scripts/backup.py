#!/usr/bin/env python3
"""Encrypted local backup. Restore test always creates its own disposable DB."""
import argparse,contextlib,datetime,hashlib,io,json,os,re,secrets,select,stat,subprocess,tarfile,time
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

COUNT_SQL="SELECT 'projects',count(*) FROM projects UNION ALL SELECT 'tasks',count(*) FROM tasks UNION ALL SELECT 'dependencies',count(*) FROM dependencies UNION ALL SELECT 'users',count(*) FROM users UNION ALL SELECT 'time_entries',count(*) FROM time_entries UNION ALL SELECT 'calendars',count(*) FROM calendars ORDER BY 1;"
COUNT_TABLES=('calendars','dependencies','projects','tasks','time_entries','users')
def counts(container, database="planner"):
 return run(['exec',container,'psql','-U','planner','-d',database,'-At','-c',COUNT_SQL]).decode().strip()

def snapshot_protocol(child, timeout=180):
 """Consume exactly one bounded snapshot/counts/ready protocol from psql."""
 deadline=time.monotonic()+timeout;pending=b'';total=0;lines=[]
 while True:
  remaining=deadline-time.monotonic()
  if remaining<=0 or not select.select([child.stdout],[],[],max(remaining,0))[0]:raise TimeoutError('backup snapshot protocol timed out')
  chunk=os.read(child.stdout.fileno(),4096)
  if not chunk:raise RuntimeError('backup snapshot owner stopped before ready')
  total+=len(chunk)
  if total>4096:raise ValueError('backup snapshot protocol exceeds limit')
  pending+=chunk
  while b'\n' in pending:
   line,pending=pending.split(b'\n',1)
   if len(line)>256 or len(lines)>=8:raise ValueError('invalid backup snapshot protocol')
   lines.append(line)
   if line==b'READY':
    if pending or len(lines)!=8:raise ValueError('invalid backup snapshot protocol')
    try:
     marker,snapshot=lines[0].decode('ascii').split('|')
     if marker!='SNAPSHOT' or not re.fullmatch(r'[0-9A-Fa-f]{8}-[0-9A-Fa-f]{8}-[0-9]{1,10}',snapshot):raise ValueError()
     for table,record in zip(COUNT_TABLES,lines[1:7]):
      name,count=record.decode('ascii').split('|')
      if name!=table or not re.fullmatch(r'[0-9]{1,19}',count):raise ValueError()
    except (ValueError,UnicodeError):raise ValueError('invalid backup snapshot protocol') from None
    return snapshot,b'\n'.join(lines[1:7]).decode('ascii')
  if len(pending)>256:raise ValueError('invalid backup snapshot protocol')

def close_snapshot_owner(child, container, application):
 """Rollback this exporter only; do not leak a server transaction on errors."""
 try:
  if child.poll() is None:
   try:child.stdin.write(b'ROLLBACK;\n\\q\n');child.stdin.flush()
   except (BrokenPipeError,OSError):pass
  try:child.stdin.close()
  except (BrokenPipeError,OSError):pass
  try:child.wait(timeout=2)
  except subprocess.TimeoutExpired:
   # Killing docker exec alone need not kill the remote psql process. Address
   # only this random application name, never another backup/user session.
   terminate_owned_backend(container,application)
   child.wait(timeout=2)
 finally:
  child.stdout.close()
  if child.poll() is None:
   child.terminate()
   try:child.wait(timeout=2)
   except subprocess.TimeoutExpired:child.kill();child.wait(timeout=2)

def terminate_owned_backend(container, application):
 """Bounded cleanup of only one randomly identified backup operation."""
 if not re.fullmatch(r'gm-vector-backup-(?:dump-)?[0-9a-f]{24}',application):raise ValueError('invalid backup operation identity')
 sql="SELECT pg_terminate_backend(pid,1000) FROM pg_stat_activity WHERE datname='planner' AND usename='planner' AND application_name='"+application+"' AND pid<>pg_backend_pid();"
 result=subprocess.run(DOCKER+['exec',container,'psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','planner','-d','planner','-c',sql],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=5)
 if result.returncode or len(result.stdout)>4096 or any(line!=b't' for line in result.stdout.splitlines()):raise RuntimeError('backup remote session cleanup failed')

@contextlib.contextmanager
def database_snapshot(container):
 application='gm-vector-backup-'+secrets.token_hex(12)
 child=subprocess.Popen(DOCKER+['exec','-i',container,'psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','planner','-d','planner'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
 try:
  sql="BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\nSET LOCAL statement_timeout='180s';\nSET LOCAL idle_in_transaction_session_timeout='185s';\nSET LOCAL application_name='"+application+"';\nSELECT 'SNAPSHOT|'||pg_export_snapshot();\n"+COUNT_SQL+"\nSELECT 'READY';\n"
  child.stdin.write(sql.encode());child.stdin.flush()
  snapshot,rows=snapshot_protocol(child)
  yield snapshot,rows
  if child.poll() is not None:raise RuntimeError('backup snapshot owner stopped during capture')
 finally:
  close_snapshot_owner(child,container,application)

def capture_database(container):
 application='gm-vector-backup-dump-'+secrets.token_hex(12)
 with database_snapshot(container) as (snapshot,rows):
  try:
   dump=run(['exec','-e','PGAPPNAME='+application,container,'pg_dump','-U','planner','-d','planner','-Fc','--no-owner','--no-acl','--snapshot='+snapshot])
  except BaseException:
   # docker exec's CLI can stop while its remote pg_dump continues. Explicitly
   # close only this operation's backend before releasing its exported snapshot.
   terminate_owned_backend(container,application)
   raise
  if len(dump)>MAX_ARCHIVE-MAX_SECRET*16:raise ValueError('database dump exceeds limit')
 return dump,rows

def process_lines(argv, *, timeout=180, max_line=MAX_SECRET*2+64, max_output=MAX_ARCHIVE*3):
 """Read a fixed internal protocol without buffering arbitrary DB output."""
 child=subprocess.Popen(DOCKER+argv,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
 deadline=time.monotonic()+timeout;pending=b'';total=0
 try:
  while True:
   remaining=deadline-time.monotonic()
   if remaining<=0:raise TimeoutError('backup verification subprocess timed out')
   if not select.select([child.stdout],[],[],remaining)[0]:raise TimeoutError('backup verification subprocess timed out')
   chunk=os.read(child.stdout.fileno(),65536)
   if not chunk:break
   total+=len(chunk)
   if total>max_output:raise ValueError('backup verification output exceeds limit')
   pending+=chunk
   while b'\n' in pending:
    line,pending=pending.split(b'\n',1)
    if len(line)>max_line:raise ValueError('backup verification record exceeds limit')
    yield line
   if len(pending)>max_line:raise ValueError('backup verification record exceeds limit')
  if pending:raise ValueError('incomplete backup verification record')
  if child.wait(timeout=max(.001,deadline-time.monotonic())):raise RuntimeError('backup verification subprocess failed')
 finally:
  child.stdout.close()
  if child.poll() is None:
   child.terminate()
   try:child.wait(timeout=2)
   except subprocess.TimeoutExpired:
    child.kill();child.wait(timeout=2)

def verify_restored_secrets(container, application_key):
 """Authenticate existing sealed fields in the disposable restored database.

 Nothing decrypted is returned, printed or written. This is a restore-tool check,
 not a change to application readiness or a public diagnostic API.
 """
 sql="SELECT 'user_totp',encode(totp,'hex') FROM users WHERE octet_length(totp)>0 UNION ALL SELECT 'enrollment',encode(payload,'hex') FROM enrollments WHERE payload IS NOT NULL AND octet_length(payload)>0 UNION ALL SELECT 'invitation',encode(decode(payload->>'secret','base64'),'hex') FROM outbox WHERE provider='smtp_invitation' AND payload ? 'secret';"
 totals={'user_totp':0,'enrollment':0,'invitation':0};cipher=AESGCM(application_key)
 records=process_lines(['exec',container,'psql','-X','-qAt','-U','planner','-d','planner','-v','ON_ERROR_STOP=1','-c',sql])
 try:
  for record in records:
   try:
    category,encoded=record.decode('ascii').split('|')
    if category not in totals or not encoded:raise ValueError()
    sealed=bytes.fromhex(encoded)
    if len(sealed)<28:raise ValueError()
    cipher.decrypt(sealed[:12],sealed[12:],b'GMA|Planner:v1')
   except Exception:
    raise ValueError('restored application secrets could not be authenticated') from None
   totals[category]+=1
 finally:
  records.close()
 return totals
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
 dump,snapshot_counts=capture_database(args.container)
 values['database.dump']=dump
 meta={'created_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'dump_sha256':hashlib.sha256(dump).hexdigest(),'counts':snapshot_counts,'format':2,'files':{n:{'size':len(v),'sha256':hashlib.sha256(v).hexdigest()} for n,v in values.items()}}
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
  sealed_fields=verify_restored_secrets(name,values['secrets/encryption.key']) if complete else None
  if complete:runtime_smoke(args,name,values)
  print(json.dumps({'restore_verified':True,'runtime_verified':complete,'legacy_database_only':not complete,'sealed_fields_verified':sealed_fields,'seconds':round(time.monotonic()-started,2),'counts':meta['counts'].splitlines()}))
 finally:
  run(['rm','-f',name])
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--config',default=str(Path.home()/'.config/gm-vector'));p.add_argument('--key',default=str(Path.home()/'.config/gm-vector-backup/key'));p.add_argument('--destination',default=str(Path.home()/'.local/share/gm-vector/backups'));p.add_argument('--container',required=True,help='Explicit PostgreSQL container to back up; never inferred');p.add_argument('--planner-bin',default=str(ROOT/'production/bin/planner'));p.add_argument('--verify',action='store_true');p.add_argument('--verify-file');a=p.parse_args()
 if a.verify_file:verify(a,a.verify_file)
 else:
  path=backup(a)
  if a.verify:verify(a,path)
