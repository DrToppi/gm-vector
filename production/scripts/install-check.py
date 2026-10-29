#!/usr/bin/env python3
"""Disposable end-to-end Docker installation; never reads an existing workspace."""
import base64, hashlib, hmac, importlib.util, json, os, secrets, socket, ssl, struct, subprocess, tempfile, time
from pathlib import Path
from urllib.parse import urlsplit
from urllib.request import Request, urlopen
ROOT=Path(__file__).resolve().parents[1]
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
  m.prepare(state,manifest)
  db=m.run(compose+['ps','-q','db'],env,capture=True)
  query=lambda sql:m.run(m.docker()+['exec',db,'psql','-U','planner','-d','planner','-Atc',sql],capture=True)
  assert query('SELECT count(*) FROM projects')=='0','unexpected seeded project'
  assert query('SELECT count(*) FROM users')=='0','unexpected seeded account'
  pem=(state/'tls/fullchain.pem').read_text()
  trust=ssl.create_default_context(cadata=pem);origin='https://localhost:'+str(port)
  def call(path,body=None):
   req=Request(origin+'/api/v1'+path,data=None if body is None else json.dumps(body).encode(),headers={'Origin':origin,'Content-Type':'application/json'})
   with urlopen(req,context=trust,timeout=15) as r:return json.load(r)
  assert call('/auth/status')['setup_required'] is True
  assert not (state/'config/bootstrap.token').exists()
  assert not (state/'setup-url.txt').exists()
  enrolled=call('/auth/bootstrap',{'username':'synthetic-admin','name':'Synthetic administrator','password':secrets.token_urlsafe(32)})
  confirmed=call('/auth/bootstrap/confirm',{'token':enrolled['enrollment_token'],'code':otp(enrolled['secret'])})
  assert len(confirmed['recovery_codes'])>=1
  assert call('/auth/status')['setup_required'] is False
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
  print('PASS: empty workspace, verified HTTPS, admin/TOTP setup, repeated upgrade, unchanged secrets, local recovery and credential revocation.')
 finally:
  subprocess.run(compose+['down','--volumes','--remove-orphans'],env=env,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
