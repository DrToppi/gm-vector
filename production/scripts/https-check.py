#!/usr/bin/env python3
"""Synthetic Caddy certificate regression. Never uses installation configuration."""
import argparse, importlib.util, json, os, socket, ssl, subprocess, tempfile, time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('demo_tls',ROOT/'deploy/install.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
def run(args,env=None):
 return subprocess.run(args,env=env,capture_output=True,text=True,timeout=30)
def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--caddy',type=Path,default=ROOT/'bin/caddy');a=p.parse_args()
 if os.getuid()==0:raise RuntimeError('run this check as an unprivileged user')
 with tempfile.TemporaryDirectory(prefix='gm-tls-test-') as temp:
  d=Path(temp);d.chmod(0o700);env={**os.environ,'HOME':str(d),'XDG_DATA_HOME':str(d/'data'),'XDG_CONFIG_HOME':str(d/'config'),'GMA_HTTPS_HOST':'planner.example.test','GMA_PUBLIC_ORIGIN':'https://planner.example.test'}
  def openssl(*args):
   r=run(['openssl',*map(str,args)]);assert r.returncode==0,'synthetic certificate generation failed'
  openssl('req','-x509','-newkey','rsa:2048','-nodes','-keyout',d/'ca.key','-out',d/'ca.pem','-days','1','-subj','/CN=Synthetic test CA','-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign')
  openssl('req','-newkey','rsa:2048','-nodes','-keyout',d/'privkey.pem','-out',d/'server.csr','-subj','/CN=planner.example.test')
  (d/'ext').write_text('subjectAltName=DNS:planner.example.test\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n')
  openssl('x509','-req','-in',d/'server.csr','-CA',d/'ca.pem','-CAkey',d/'ca.key','-CAcreateserial','-out',d/'fullchain.pem','-days','1','-extfile',d/'ext')
  (d/'privkey.pem').chmod(0o600)
  https,http,upstream=port(),port(),port()
  template=(ROOT/'tls/Caddyfile.files').read_text().replace('/etc/caddy/certs/',str(d)+'/').replace(':9443',':'+str(https)).replace(':9080',':'+str(http)).replace('api:8080','127.0.0.1:'+str(upstream))
  template+='\nhttp://:'+str(upstream)+' {\n respond /readyz `{"ready":true}` 200\n respond /client "{http.request.header.X-GMA-Client-IP}"\n}\n'
  config=d/'Caddyfile';config.write_text(template)
  valid=run([str(a.caddy),'validate','--config',str(config),'--adapter','caddyfile'],env);assert valid.returncode==0,'valid certificate rejected'
  bad=d/'bad';bad.write_text(template.replace(str(d/'privkey.pem'),str(d/'ca.key')))
  assert run([str(a.caddy),'validate','--config',str(bad),'--adapter','caddyfile'],env).returncode!=0,'mismatched key accepted'
  acme=run([str(a.caddy),'adapt','--config',str(ROOT/'tls/Caddyfile.acme'),'--adapter','caddyfile'],env);assert acme.returncode==0,'ACME template invalid'
  parsed=json.loads(acme.stdout);assert any(':9443' in server.get('listen',[]) for server in parsed['apps']['http']['servers'].values())
  process=subprocess.Popen([str(a.caddy),'run','--config',str(config),'--adapter','caddyfile'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
  try:
   trust=ssl.create_default_context(cafile=str(d/'ca.pem'))
   for _ in range(100):
    connection=m.LocalTLSConnection('planner.example.test',https,'127.0.0.1',context=trust,timeout=1)
    try:
     connection.request('GET','/readyz');response=connection.getresponse()
     assert response.status==200 and json.loads(response.read())['ready'] is True;break
    except OSError as e:last_error=repr(e);time.sleep(.05)
    finally:connection.close()
   else:
    if process.poll() is not None: raise RuntimeError('synthetic Caddy startup failed: '+process.stderr.read().decode())
    raise RuntimeError('synthetic HTTPS did not become ready: '+last_error)
   connection=m.LocalTLSConnection('planner.example.test',https,'127.0.0.1',context=trust,timeout=2)
   try:
    connection.request('GET','/client',headers={'X-GMA-Client-IP':'spoofed'});assert connection.getresponse().read()==b'127.0.0.1'
   finally:connection.close()
   for name,ctx in [('wrong.example.test',trust),('planner.example.test',ssl.create_default_context())]:
    connection=m.LocalTLSConnection(name,https,'127.0.0.1',context=ctx,timeout=2)
    try:
     connection.request('GET','/readyz');raise AssertionError('invalid certificate trust/hostname accepted')
    except ssl.SSLError:pass
    finally:connection.close()
  finally:
   process.terminate()
   try:process.wait(timeout=10)
   except subprocess.TimeoutExpired:process.kill();process.wait()
 print('PASS: non-root Caddy HTTPS, matching cert/key, wrong-key rejection, CA/hostname validation, trusted proxy header, ACME adaptation (no issuance).')
if __name__=='__main__':main()
