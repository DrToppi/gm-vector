#!/usr/bin/env python3
"""Real Docker recovery verification with disposable data; no production config."""
import base64,datetime,hashlib,importlib.util,json,os,secrets,shlex,subprocess,tempfile,time,types
from pathlib import Path
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes,serialization
from cryptography.hazmat.primitives.asymmetric import ed25519,rsa
spec=importlib.util.spec_from_file_location('backup',Path(__file__).with_name('backup.py'));b=importlib.util.module_from_spec(spec);spec.loader.exec_module(b)
ROOT=Path(__file__).resolve().parents[2]
def write(path,data):
 path.write_bytes(data if isinstance(data,bytes) else data.encode());path.chmod(0o600)
with tempfile.TemporaryDirectory(prefix='gma-recovery-regression-') as directory:
 root=Path(directory);cfg=root/'config';cfg.mkdir(mode=0o700);dest=root/'copied';dest.mkdir(mode=0o700)
 for n in b.CORE:write(cfg/n,b'S'*32 if n=='encryption.key' else b'Z'*48)
 runtime={'database_url':'postgres://planner@127.0.0.1:5432/planner?sslmode=disable','origin':'https://localhost:9443','web_dir':'/app/web','planning_timezone':'America/New_York',**{field:'/run/secrets/'+name for field,name in b.RUNTIME_FILES.items() if field not in ('report_public_key_file','updater_token_file')}}
 write(cfg/'config.json',json.dumps(runtime));write(cfg/'ldap.json','{"enabled":false}')
 ca_key=rsa.generate_private_key(public_exponent=65537,key_size=2048);subject=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'synthetic-recovery')]);now=datetime.datetime.now(datetime.timezone.utc)
 ca=x509.CertificateBuilder().subject_name(subject).issuer_name(subject).public_key(ca_key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(days=1)).not_valid_after(now+datetime.timedelta(days=1)).add_extension(x509.BasicConstraints(ca=True,path_length=None),critical=True).sign(ca_key,hashes.SHA256())
 write(cfg/'smtp-ca.pem',ca.public_bytes(serialization.Encoding.PEM));write(cfg/'smtp.json',json.dumps({'enabled':True,'host':'127.0.0.1','port':2525,'security':'tls','from':'sender@example.test','username':'synthetic','password':'SMTP-MARKER','ca_file':'/run/secrets/smtp-ca.pem'}))
 write(cfg/'report-public-key.json','PUBLIC-MARKER');write(cfg/'support-private.pem','PRIVATE-MUST-NOT-BACKUP');write(cfg/'signing.key','ISSUER-MUST-NOT-BACKUP')
 for filename in ['smtp.json','smtp-ca.pem','ldap.json','report-public-key.json']:(cfg/filename).rename(cfg/(filename+'.saved'))
 # Verify actual loader fails at configuration stage, before attempting its DB.
 dangling=dict(runtime)
 for field,filename in b.RUNTIME_FILES.items():
  if dangling.get(field):dangling[field]=str(cfg/filename)
 write(cfg/'dangling.json',json.dumps(dangling))
 probe=subprocess.run([str(ROOT/'production/bin/planner'),'api','--config',str(cfg/'dangling.json')],capture_output=True,timeout=15)
 assert probe.returncode!=0 and b'configuration unavailable' in probe.stderr
 # Dangling configured paths cause collection failure, not stale-volume fallback.
 try:b.collect_secrets(cfg)
 except FileNotFoundError:pass
 else:raise AssertionError('missing configured secrets accepted')
 for filename in ['smtp.json','smtp-ca.pem','ldap.json','report-public-key.json']:(cfg/(filename+'.saved')).rename(cfg/filename)
 name='gma-recovery-source-'+secrets.token_hex(6)
 try:
  b.run(['run','-d','--name',name,'--network','none','--tmpfs','/var/lib/postgresql','-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_USER=planner','-e','POSTGRES_DB=planner',b.DB_IMAGE])
  for _ in range(40):
   try:b.run(['exec',name,'psql','-h','127.0.0.1','-U','planner','-d','planner','-Atc','SELECT 1']);break
   except RuntimeError:time.sleep(.25)
  schema=ROOT/'production/internal/planner'
  sql=(schema/'schema.sql').read_bytes()+b'\n'+b'\n'.join(p.read_bytes() for p in sorted(schema.glob('schema-*.sql')))
  fixture=subprocess.run(b.DOCKER+['exec','-i',name,'psql','-U','planner','-d','planner','-v','ON_ERROR_STOP=1'],input=sql,capture_output=True,timeout=30)
  if fixture.returncode:raise RuntimeError('Synthetic schema fixture failed: '+fixture.stderr.decode(errors='replace')[-2000:])
  # Date-only retained history deliberately predates this project's planning year.
  days={}
  day=datetime.date(2026,1,1)
  while day.year==2026:
   days[day.isoformat()]={'working':day.weekday()<5,'shortened':False}
   day+=datetime.timedelta(days=1)
  calendar_json=json.dumps(days,separators=(',',':'))
  calendar_hash=hashlib.sha256(calendar_json.encode()).hexdigest()
  session=secrets.token_urlsafe(32)
  session_hash=hashlib.sha256(session.encode()).hexdigest()
  history_sql=f"""
INSERT INTO calendars(id,year,title,source,checksum,days) VALUES('recovery-cal-2026',2026,'Recovery calendar','synthetic','{calendar_hash}','{calendar_json}');
INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES('recovery-project','Recovery history','2026-01-05','working','recovery-cal-2026');
INSERT INTO tasks(project_id,id,title,status,legacy_actual_minutes) VALUES('recovery-project','recovery-root','Root','todo',23);
INSERT INTO tasks(project_id,id,parent_id,title,status,legacy_actual_minutes) VALUES('recovery-project','recovery-child','recovery-root','Child','todo',17);
INSERT INTO employees(id,name,daily_minutes) VALUES('recovery-employee','Historical employee',480);
INSERT INTO time_entries(id,project_id,task_id,employee_id,work_date,minutes,comment)
 SELECT 'recovery-entry-'||lpad(n::text,4,'0'),'recovery-project',CASE WHEN n=1 THEN 'recovery-root' ELSE 'recovery-child' END,CASE WHEN n=1 THEN NULL ELSE 'recovery-employee' END,'2024-02-29',1,'Retained fact' FROM generate_series(1,1001) AS n;
INSERT INTO users(id,username,name,password_hash,totp,admin,active) VALUES('recovery-admin','recovery-admin','Recovery administrator','unused',decode('00','hex'),true,true);
INSERT INTO sessions(hash,user_id,csrf) VALUES('{session_hash}','recovery-admin','synthetic-csrf');
"""
  b.run(['exec','-i',name,'psql','-U','planner','-d','planner','-v','ON_ERROR_STOP=1'],history_sql.encode())
  digest_sql="SELECT md5(coalesce(jsonb_agg(to_jsonb(t) ORDER BY t.id)::text,'[]')) FROM time_entries t;"
  source_digest=b.run(['exec',name,'psql','-U','planner','-d','planner','-Atc',digest_sql]).strip()
  args=types.SimpleNamespace(config=str(cfg),destination=str(root/'backups'),key=str(root/'backup.key'),container=name,planner_bin=str(ROOT/'production/bin/planner'))
  archive=b.backup(args);values,meta,complete=b.read_archive(args,archive);assert complete
  assert json.loads(values['secrets/config.json'])['planning_timezone']=='America/New_York'
  assert b'SMTP-MARKER' in values['secrets/smtp.json'];assert 'secrets/smtp-ca.pem' in values and 'secrets/ldap.json' in values
  assert not any(b'PRIVATE-MUST-NOT-BACKUP' in data or b'ISSUER-MUST-NOT-BACKUP' in data for data in values.values())
  assert b'SMTP-MARKER' not in archive.read_bytes()
  original_smoke=b.runtime_smoke
  def observed_smoke(args,container,files):
   assert b.run(['inspect',container,'--format','{{.HostConfig.NetworkMode}}']).strip()==b'none'
   b.run(['exec','-d',container,'sh','-c','nc -l -p 2525 > /tmp/gma-smtp-contact'])
   original_smoke(args,container,files)
   assert b.run(['exec',container,'psql','-U','planner','-d','planner','-Atc',digest_sql]).strip()==source_digest
   headers=['--header=Host: localhost:9443','--header=Cookie: __Host-gma='+session]
   def restored_json(path):
    return json.loads(b.run(['exec',container,'wget','-qO-',*headers,'http://127.0.0.1:18080/api/v1'+path]))
   assert restored_json('/auth/me')['planning_timezone']=='America/New_York'
   history=restored_json('/projects/recovery-project/time-history?task_id=recovery-root&limit=100')
   assert history['total_count']==1001 and history['recorded_minutes']==1001
   assert history['planning_timezone']=='America/New_York' and len(history['items'])==100 and history['next_cursor']
   assert history['legacy_unattributed']['minutes']==40
   assert history['items'][0]['date']=='2024-02-29' and history['items'][0]['employee_id']==''
   b.run(['exec',container,'sh','-c','test ! -s /tmp/gma-smtp-contact'])
   assert b.run(['exec',container,'psql','-U','planner','-d','planner','-Atc',"SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname='restore_runtime'"]).strip()==b'f'
  b.runtime_smoke=observed_smoke
  b.verify(args,archive)

  legacy={n:data for n,data in values.items() if n in {'database.dump'}|{'secrets/'+n for n in b.CORE}}
  legacy_meta={k:v for k,v in meta.items() if k!='files'};legacy_meta['format']=1;legacy['manifest.json']=json.dumps(legacy_meta).encode()
  buf=b.io.BytesIO()
  with b.tarfile.open(fileobj=buf,mode='w') as tar:
   for filename,data in legacy.items():
    item=b.tarfile.TarInfo(filename);item.size=len(data);tar.addfile(item,b.io.BytesIO(data))
  nonce=secrets.token_bytes(12);legacy_path=root/'legacy.gma';write(legacy_path,b'GMABK01'+nonce+b.AESGCM(b.key(Path(args.key))).encrypt(nonce,buf.getvalue(),b'GMA|Planner backup v1'))
  assert b.read_archive(args,legacy_path)[2] is False;b.verify(args,legacy_path)
  damaged=bytearray(archive.read_bytes());damaged[-1]^=1;write(root/'tampered.gma',bytes(damaged))
  try:b.read_archive(args,root/'tampered.gma')
  except Exception as exc:assert type(exc).__name__=='InvalidTag'
  else:raise AssertionError('damaged backup accepted')
  print('PASS: configured integration recovery, restored planning timezone and1001 retained facts/date digests, actual API/non-superuser/network-none, tamper rejection, required dependencies and private-key exclusion')
 finally:b.run(['rm','-f',name])
