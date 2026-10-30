#!/usr/bin/env python3
"""Disposable end-to-end Docker installation; never reads an existing workspace."""
import argparse, base64, hashlib, hmac, http.client, http.cookiejar, importlib.util, json, os, secrets, socket, ssl, struct, subprocess, tempfile, time
from pathlib import Path
from urllib.parse import urlsplit
from urllib.error import HTTPError
from urllib.request import Request, build_opener, HTTPSHandler, HTTPCookieProcessor, ProxyHandler
ROOT=Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--app-image',help='explicit isolated candidate image; default uses the existing build tag')
parser.add_argument('--evidence-file',type=Path,help='new private log file; existing files are refused')
options=parser.parse_args()
if options.app_image:os.environ['GM_VECTOR_APP_IMAGE']=options.app_image
if options.evidence_file:
 fd=os.open(options.evidence_file,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 try:os.dup2(fd,1);os.dup2(fd,2)
 finally:os.close(fd)
spec=importlib.util.spec_from_file_location('installer',ROOT/'deploy/install.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
def free_port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def otp(secret):
 digest=hmac.new(base64.b32decode(secret+'='*((-len(secret))%8)),struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest();i=digest[-1]&15
 return str((struct.unpack('>I',digest[i:i+4])[0]&0x7fffffff)%1000000).zfill(6)
with tempfile.TemporaryDirectory(prefix='gm-vector-install-check-') as temp:
 state=Path(temp)/'state';port=free_port();m.initialize(state,port,'172.30.96.0/29');manifest=m.package();compose,env=m.context(state,manifest)
 try:
  original={n:(state/'config'/n).read_bytes() for n in ('encryption.key','instance.id','database.password')}
  tls_original={n:(state/'tls'/n).read_bytes() for n in ('fullchain.pem','privkey.pem')}
  runtime_path=state/'config/config.json'
  runtime=json.loads(runtime_path.read_text())
  assert runtime['planning_timezone']=='Europe/Moscow'
  runtime['planning_timezone']='America/New_York'
  runtime_path.write_text(json.dumps(runtime))
  m.prepare(state,manifest)
  db=m.run(compose+['ps','-q','db'],env,capture=True)
  query=lambda sql:m.run(m.docker()+['exec',db,'psql','-U','planner','-d','planner','-Atc',sql],capture=True)
  assert query('SELECT count(*) FROM projects')=='0','unexpected seeded project'
  assert query('SELECT count(*) FROM users')=='0','unexpected seeded account'
  pem=(state/'tls/fullchain.pem').read_text()
  trust=ssl.create_default_context(cadata=pem);origin='https://localhost:'+str(port)
  cookies=http.cookiejar.CookieJar()
  opener=build_opener(ProxyHandler({}),HTTPSHandler(context=trust),HTTPCookieProcessor(cookies))
  def call(path,body=None,csrf=''):
   req=Request(origin+'/api/v1'+path,data=None if body is None else json.dumps(body).encode(),headers={'Origin':origin,'Content-Type':'application/json','X-CSRF-Token':csrf})
   with opener.open(req,timeout=15) as r:return json.load(r)
  assert call('/auth/status')['setup_required'] is True
  assert not (state/'config/bootstrap.token').exists()
  assert not (state/'setup-url.txt').exists()
  enrolled=call('/auth/bootstrap',{'username':'synthetic-admin','name':'Synthetic administrator','password':secrets.token_urlsafe(32)})
  confirmed=call('/auth/bootstrap/confirm',{'token':enrolled['enrollment_token'],'code':otp(enrolled['secret'])})
  assert len(confirmed['recovery_codes'])>=1
  me=call('/auth/me')
  assert me['planning_timezone']=='America/New_York'
  assert call('/auth/status')['setup_required'] is False
  # Legal worst-size comments exercise the actual 768MiB API and 32MiB proxy tmpfs,
  # not only an in-process httptest server. An unread body holds a download
  # while a different request checks ordinary reads and the single-export slot.
  query("INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) SELECT 'history-image-fixture','Packaged history fixture','2026-01-05','working',id FROM calendars LIMIT 1;")
  query("INSERT INTO tasks(project_id,id,title,status,responsibility,requested_start,duration_days) VALUES('history-image-fixture','history-image-task','History task','todo','customer','2026-01-05',1);")
  query("INSERT INTO time_entries(id,project_id,task_id,work_date,minutes,comment) SELECT 'image-fact-'||lpad(n::text,5,'0'),'history-image-fixture','history-image-task','2024-02-29',1,repeat('x',4000) FROM generate_series(1,10000) AS n;")
  path='/api/v1/projects/history-image-fixture/time-history/export'
  connection=http.client.HTTPSConnection('localhost',port,context=trust,timeout=30)
  def assert_export_busy():
   try:
    with opener.open(Request(origin+path),timeout=15):
     raise AssertionError('another export bypassed the held download slot')
   except HTTPError as busy:
    assert busy.code==429 and json.load(busy)['error']['code']=='history_export_busy'
  export_deadline=time.monotonic()+40
  try:
   connection.connect()
   connection.request('GET',path,headers={'Cookie':'; '.join(c.name+'='+c.value for c in cookies)})
   response=connection.getresponse()
   assert response.status==200
   assert_export_busy()
   assert call('/projects/history-image-fixture')['project']['id']=='history-image-fixture'
   written=call('/employees',{'id':'image-fixture-employee','name':'Packaged write fixture','daily_minutes':480},me['csrf'])
   assert written['id']=='image-fixture-employee'
   api_container=m.run(compose+['ps','-q','api'],env,capture=True)
   assert m.run(m.docker()+['inspect','--format','{{.HostConfig.Memory}}',api_container],capture=True)==str(768*1024*1024)
   assert_export_busy()
   declared=response.getheader('Content-Length')
   if declared is not None:assert 0<=int(declared)<=64*1024*1024
   chunks=[];export_bytes=0
   while True:
    remaining=export_deadline-time.monotonic()
    if remaining<=0:raise TimeoutError('packaged export exceeded its absolute fixture deadline')
    if connection.sock is not None:connection.sock.settimeout(remaining)
    chunk=response.read1(min(1024*1024,64*1024*1024+1-export_bytes))
    if not chunk:break
    export_bytes+=len(chunk)
    assert export_bytes<=64*1024*1024,'packaged export exceeded its byte guard'
    chunks.append(chunk)
   if declared is not None:assert export_bytes==int(declared),'partial packaged export body'
   assert 32*1024*1024<export_bytes<=64*1024*1024
   raw=b''.join(chunks);del chunks
   document=json.loads(raw);del raw
   assert document['total_count']==10000 and document['recorded_minutes']==10000
   assert len(document['items'])==10000
   assert {row['id'] for row in document['items']}=={'image-fact-'+str(n).zfill(5) for n in range(1,10001)}
   assert all(row['date']=='2024-02-29' and row['minutes']==1 and row['comment']=='x'*4000 for row in document['items'])
   assert document['planning_timezone']=='America/New_York'
   state_after=json.loads(m.run(m.docker()+['inspect','--format','{{json .State}}',api_container],capture=True))
   assert state_after['Running'] and not state_after['OOMKilled']
   assert m.run(m.docker()+['inspect','--format','{{.RestartCount}}',api_container],capture=True)=='0'
   print(json.dumps({'history_export_rows':10000,'history_export_bytes':export_bytes,'api_memory_limit_bytes':768*1024*1024,'slow_download_busy_guard':True,'concurrent_project_read':True,'concurrent_capacity_write':True,'oom_or_restart':False}),flush=True)
  finally:connection.close()
  query("DELETE FROM projects WHERE id='history-image-fixture';")
  query("DELETE FROM employees WHERE id='image-fixture-employee';")
  # The actual nonroot distroless API/worker, not the host Go installation, must
  # resolve each accepted IANA zone. No host timezone/tzdata is mounted into it.
  def stage_runtime():
   runtime_path.write_text(json.dumps(runtime))
   assert runtime_path.stat().st_mode&0o777==0o600
   m.run(compose+['run','--rm','--no-deps','init-secrets'],env)
  def restart_runtime():
   m.run(compose+['up','-d','--no-deps','--force-recreate','api','worker'],env)
   m.wait_web(state,compose,env)
   def runtime_states():
    states={}
    for service in ('api','worker'):
     container=m.run(compose+['ps','-a','-q',service],env,capture=True)
     assert container and '\n' not in container
     assert m.run(m.docker()+['inspect','--format','{{.Image}}',container],capture=True)==manifest['images']['app']
     assert m.run(m.docker()+['inspect','--format','{{.Config.User}}',container],capture=True)=='65532:65532'
     status=json.loads(m.run(m.docker()+['inspect','--format','{{json .State}}',container],capture=True))
     assert status['Running'] and status['Status']=='running'
     assert m.run(m.docker()+['inspect','--format','{{.RestartCount}}',container],capture=True)=='0'
     states[service]=status['StartedAt']
    return states
   starts=runtime_states();time.sleep(1)
   assert runtime_states()==starts,'runtime restarted during the observed startup window'
  for zone in ('UTC','Asia/Kathmandu','Europe/Moscow'):
   runtime['planning_timezone']=zone
   stage_runtime();restart_runtime()
   assert call('/auth/me')['planning_timezone']==zone
  runtime['planning_timezone']='Etc/DefinitelyMissing'
  stage_runtime()
  # Recreate the same services used by both positive controls, rather than a
  # one-off container that would contend for the API's fixed edge address.
  m.run(compose+['up','-d','--no-deps','--force-recreate','api','worker'],env)
  for service in ('api','worker'):
   container=m.run(compose+['ps','-a','-q',service],env,capture=True)
   assert container and '\n' not in container
   deadline=time.monotonic()+10
   while True:
    status=json.loads(m.run(m.docker()+['inspect','--format','{{json .State}}',container],capture=True))
    if status['Status'] in ('exited','restarting') and status['ExitCode']!=0:break
    assert time.monotonic()<deadline,'invalid planning timezone did not stop the packaged runtime'
    time.sleep(.1)
   assert status['ExitCode']!=0
   logs=subprocess.run(m.docker()+['logs',container],capture_output=True,timeout=15)
   assert logs.returncode==0
   assert b'configuration unavailable' in logs.stdout+logs.stderr,'unexpected failure instead of timezone validation'
  # Old configurations remain valid: omission chooses the compatible policy.
  runtime.pop('planning_timezone')
  stage_runtime();restart_runtime()
  assert call('/auth/me')['planning_timezone']=='Europe/Moscow'
  m.prepare(state,manifest)
  assert not (state/'config/bootstrap.token').exists()
  assert not (state/'setup-url.txt').exists()
  assert all((state/'config'/n).read_bytes()==v for n,v in original.items()),'repeat prepare changed secrets'
  assert all((state/'tls'/n).read_bytes()==v for n,v in tls_original.items()),'repeat prepare changed TLS credentials'
  assert query('SELECT count(*) FROM projects')=='0'
  assert query('SELECT count(*) FROM users WHERE active')=='1'
  recovered=subprocess.run([os.sys.executable,str(ROOT/'deploy/install.py'),'recover-admin','--state',str(state),'--username','synthetic-admin'],check=True,capture_output=True)
  links=list(state.glob('recovery-*/recovery-url.txt'));assert len(links)==1
  assert links[0].stat().st_mode&0o777==0o600
  assert query('SELECT count(*) FROM sessions')=='0'
  assert query('SELECT count(*) FROM api_tokens')=='0'
  recovery=links[0].read_text().strip();assert urlsplit(recovery).fragment.startswith('setup=')
  assert recovery.encode() not in recovered.stdout+recovered.stderr,'installer printed recovery secret'
  assert urlsplit(recovery).fragment[6:].encode() not in recovered.stdout+recovered.stderr,'installer printed token'
  assert query("SELECT count(*) FROM users WHERE active")=='0'
  print('PASS: empty workspace, verified HTTPS, admin/TOTP setup, four packaged planning zones/invalid-zone rejection/legacy default, repeated upgrade, unchanged secrets, local recovery and credential revocation.')
 finally:
  subprocess.run(compose+['down','--volumes','--remove-orphans'],env=env,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
