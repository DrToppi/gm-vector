#!/usr/bin/env python3
"""Real host IPC, maintenance and DB rollback using a disposable installation."""
import argparse, base64, hashlib, hmac, http.cookiejar, importlib.util, json, os, secrets, socket, ssl, struct, subprocess, sys, tempfile, time
from pathlib import Path
from urllib.request import Request, build_opener, HTTPSHandler, HTTPCookieProcessor
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--fetch-ref');parser.add_argument('--baseline-source',type=Path);parser.add_argument('--multiyear',action='store_true');args=parser.parse_args()
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT/'deploy'))
import update_engine as ue
import install as m

def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def otp(secret):
 digest=hmac.new(base64.b32decode(secret+'='*((-len(secret))%8)),struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest();i=digest[-1]&15
 return str((struct.unpack('>I',digest[i:i+4])[0]&0x7fffffff)%1000000).zfill(6)
with tempfile.TemporaryDirectory(prefix='gm-vector-updater-fixture-') as temp:
 state=Path(temp)/'state';m.initialize(state,port(),'172.30.97.0/29');manifest=m.package();compose,env=m.context(state,manifest)
 server=None;engine=None
 try:
  m.prepare(state,manifest)
  # Exercise documented configuration, including restrictive service umask.
  subprocess.run([sys.executable,str(ROOT/'deploy/configure-updater.py'),'--state',str(state),'--source',str(args.baseline_source or ROOT.parent),'--unit-dir',str(Path(temp)/'units')],check=True,capture_output=True,umask=0o077)
  assert (state/'updater/shared').stat().st_mode&0o777==0o755
  m.prepare(state,manifest);compose,env=m.context(state,manifest)
  db=m.run(compose+['ps','-q','db'],env,capture=True)
  def query(sql):return m.run(m.docker()+['exec',db,'psql','-U','planner','-d','planner','-Atc',sql],capture=True)
  query("INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) SELECT 'update-fixture','Synthetic upgrade fixture','2026-01-01','working',id FROM calendars LIMIT 1;")
  before=query("SELECT title FROM projects WHERE id='update-fixture'")
  if args.multiyear:
   query("INSERT INTO tasks(project_id,id,title,status,responsibility,requested_start,duration_days) VALUES('update-fixture','legacy-long-task','Synthetic legacy long task','todo','customer','2026-01-01',730);")
  trust=ssl.create_default_context(cafile=str(state/'tls/fullchain.pem'));origin=json.loads((state/'config/config.json').read_text())['origin']
  opener=build_opener(HTTPSHandler(context=trust),HTTPCookieProcessor(http.cookiejar.CookieJar()))
  def call(path,body=None,csrf='',method=None):
   req=Request(origin+'/api/v1'+path,data=None if body is None else json.dumps(body).encode(),method=method,headers={'Origin':origin,'Content-Type':'application/json','X-CSRF-Token':csrf})
   with opener.open(req,timeout=10) as response:return json.load(response)
  enrolled=call('/auth/bootstrap',{'username':'synthetic-updater-admin','name':'Synthetic updater administrator','password':'synthetic long password for update'})
  call('/auth/bootstrap/confirm',{'token':enrolled['enrollment_token'],'code':otp(enrolled['secret'])})
  totp_before=query("SELECT encode(totp,'hex') FROM users WHERE username='synthetic-updater-admin'")
  server=subprocess.Popen([sys.executable,str(ROOT/'deploy/update_service.py'),'serve','--state',str(state),'--source',str(ROOT.parent)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  for _ in range(40):
   if (state/'updater/shared/agent.sock').exists():break
   if server.poll() is not None:raise RuntimeError('fixture service stopped')
   time.sleep(.1)
  assert call('/admin/updates')['enabled'] is True,'real Docker API to host Unix IPC failed'
  server.terminate();server.wait(timeout=10);server=None
  engine=ue.Engine(state,ROOT.parent)
  previous=engine.manifest();sha=previous['commit']
  engine.record={'state':'ready','id':secrets.token_hex(16),'commit':sha,'source':str(ROOT.parent),'previous_source':str(ROOT.parent),'previous':previous,'images':manifest['images']}
  if args.fetch_ref:
   engine.record={'state':'checking','id':secrets.token_hex(16),'ref':args.fetch_ref}
   engine.check()
   assert engine.status()['state']=='ready'
   if args.baseline_source:
    assert engine.record['previous']['commit']!=engine.record['commit'],'forward upgrade requires different commits'
    assert engine.record['previous']['images']['app']!=engine.record['images']['app'],'forward upgrade requires image replacement'
  else:
   engine.snapshot('rehearsal');engine.rehearse()
  engine.apply()
  manifest={'images':engine.record['images']}
  assert engine.status()['state']=='succeeded';assert query("SELECT title FROM projects WHERE id='update-fixture'")==before
  if args.multiyear:
   migrated=call('/projects/update-fixture')['project']
   task=migrated['tasks'][0]
   assert task['duration_days']==730 and task['end']>='2028-01-01'
   assert 2027 in migrated['provisional_years']
   assert query("SELECT count(*) FROM schema_versions WHERE version=8")=='1'
   task['duration_days']=731
   for key in ('loads','warnings','provisional_years','calculation_state'):migrated.pop(key,None)
   me=call('/auth/me');csrf=me['csrf']
   saved=call('/projects/update-fixture',{'project':migrated,'override_reason':''},csrf,method='PUT')
   assert saved['tasks'][0]['duration_days']==731 and saved['tasks'][0]['end']>='2028-01-01'
  # Inject an actual post-migration DB change, then fail activation. Recovery must
  # restore the stopped-writer snapshot, not run an old image on the modified DB.
  previous=engine.manifest();sha=previous['commit'];engine.record={'state':'updating','id':secrets.token_hex(16),'commit':sha,'source':str(ROOT.parent),'previous_source':str(ROOT.parent),'previous':previous,'images':manifest['images']}
  normal_activate=engine.activate
  def broken_activate():
   query("CREATE TABLE updater_failed_migration(value text); UPDATE projects SET title='Changed by failed migration' WHERE id='update-fixture';")
   raise RuntimeError('synthetic migration failure')
  engine.activate=broken_activate;engine.execute('apply')
  assert engine.status()['state']=='recovery_required'
  assert not m.run(compose+['ps','--status','running','-q','api','worker','proxy'],env,capture=True)
  engine.activate=normal_activate;engine.recover()
  assert query("SELECT title FROM projects WHERE id='update-fixture'")==before
  assert query("SELECT to_regclass('public.updater_failed_migration') IS NULL")=='t'
  assert query("SELECT encode(totp,'hex') FROM users WHERE username='synthetic-updater-admin'")==totp_before
  assert call('/auth/status')['setup_required'] is False
  if args.multiyear: assert query("SELECT duration_days FROM tasks WHERE id='legacy-long-task'")=='731'
  assert (state/'config/encryption.key').read_bytes()==engine.archive('backup')[0]['secrets/encryption.key']
  assert engine.status()['state']=='succeeded'
  # Repeating recovery after a committed switch does not restore/switch again.
  engine.record['writers_stopped']=True;engine.recover()
  assert query("SELECT title FROM projects WHERE id='update-fixture'")==before
  print('PASS: actual host Unix IPC from nonroot API, umask077 configuration, migration rehearsal, maintenance/activation, failed-migration stopped writers, atomic DB-name rollback, TOTP/config retention and retry.')
 finally:
  if server is not None:server.terminate();server.wait(timeout=10)
  if engine is not None:os.close(engine.lock)
  subprocess.run(compose+['down','--volumes','--remove-orphans'],env=env,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
