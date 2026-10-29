import importlib.util,json,os,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('install_https',Path(__file__).with_name('install.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class HTTPSConfiguration(unittest.TestCase):
 def state(self,root):
  state=Path(root)/'state';m.initialize(state,19443,'172.30.91.0/29');return state
 def options(self,state,**changes):
  cert=state/'certs';cert.mkdir(exist_ok=True)
  for name in ('fullchain.pem','privkey.pem'):
   p=cert/name;p.write_text('synthetic placeholder');p.chmod(0o600)
  value={'mode':'files','origin':'https://planner.example.test:24443','cert_dir':str(cert),'http_port':24080}
  value.update(changes);(state/'https.json').write_text(json.dumps(value))
  config=json.loads((state/'config/config.json').read_text());config['origin']=value['origin'];(state/'config/config.json').write_text(json.dumps(config))
  return value
 def test_opt_in_and_exact_origin(self):
  with tempfile.TemporaryDirectory() as temp:
   state=self.state(temp);self.assertIsNone(m.https_options(state));self.options(state)
   options=m.https_options(state);self.assertEqual(options['hostname'],'planner.example.test')
   image='sha256:'+'a'*64
   command,env=m.context(state,{'images':dict.fromkeys(('app','db','proxy'),image)})
   self.assertIn('compose.https.files.yaml',' '.join(command));self.assertEqual(env['GMA_HTTPS_PORT'],'24443')
   self.assertEqual(env['GMA_BIND_ADDRESS'],'127.0.0.1')
   config=json.loads((state/'config/config.json').read_text());config['origin']='https://other.example.test';(state/'config/config.json').write_text(json.dumps(config))
   with self.assertRaises(ValueError):m.https_options(state)
 def test_reject_bad_origins_paths_permissions(self):
  with tempfile.TemporaryDirectory() as temp:
   state=self.state(temp)
   for origin in ('http://planner.example.test','https://x/a','https://user:pass@x','https://x?y','https://x#z','https://x\nencode gzip','https://{env.SECRET}'):
    self.options(state,origin=origin)
    with self.assertRaises(ValueError):m.https_options(state)
   self.options(state);(state/'certs/privkey.pem').chmod(0o644)
   with self.assertRaises(ValueError):m.https_options(state)
   self.options(state);p=state/'certs/privkey.pem';p.unlink();p.symlink_to(state/'certs/fullchain.pem')
   with self.assertRaises(ValueError):m.https_options(state)
 def test_acme_requires_external_issuance(self):
  with tempfile.TemporaryDirectory() as temp:
   state=self.state(temp);self.options(state,mode='acme')
   with self.assertRaises(ValueError):m.https_options(state)
 def test_custom_readiness_preserves_hostname_and_trust(self):
  from unittest.mock import Mock
  with tempfile.TemporaryDirectory() as temp:
   state=self.state(temp);self.options(state)
   connection=Mock();connection.getresponse.return_value.status=200;connection.getresponse.return_value.read.return_value=b'{"ready":true}'
   tls=m.ssl.create_default_context()
   with patch.object(m,'LocalTLSConnection',return_value=connection) as https,patch.object(m.ssl,'create_default_context',return_value=tls) as trust,patch.object(m,'run') as run:
    m.wait_web(state,[],{})
   trust.assert_called_once_with(cafile=None);run.assert_not_called()
   https.assert_called_once_with('planner.example.test',24443,'127.0.0.1',context=tls,timeout=5)
   self.assertTrue(tls.check_hostname);self.assertEqual(tls.verify_mode,m.ssl.CERT_REQUIRED)
   connection.request.assert_called_once_with('GET','/readyz',headers={'Host':'planner.example.test:24443'})
 def test_wrong_hostname_rejected_before_any_docker_operation(self):
  with tempfile.TemporaryDirectory() as temp:
   state=self.state(temp)
   env={'GMA_TLS_DIR':str(state/'tls'),'GMA_TLS_CA_FILE':str(state/'tls/fullchain.pem'),'GMA_PUBLIC_ORIGIN':'https://wrong.example.test'}
   with patch.object(m,'run') as run:
    with self.assertRaises(ValueError):m.check_https([],env)
    run.assert_not_called()
 def test_local_renewal_preserves_application_secrets(self):
  with tempfile.TemporaryDirectory() as temp:
   state=self.state(temp)
   original=(state/'tls/fullchain.pem').read_bytes()
   secret=(state/'config/encryption.key').read_bytes()
   m.local_tls(state,renew=True)
   self.assertNotEqual(original,(state/'tls/fullchain.pem').read_bytes())
   self.assertEqual(secret,(state/'config/encryption.key').read_bytes())
   self.assertEqual((state/'tls/privkey.pem').stat().st_mode&0o777,0o600)
   env={'GMA_TLS_DIR':str(state/'tls'),'GMA_TLS_CA_FILE':str(state/'tls/fullchain.pem'),'GMA_NGINX_CONFIG':str(state/'nginx.conf'),'GMA_PROXY_IMAGE':'sha256:'+'a'*64}
   with patch.object(m,'run') as run:m.check_https([],env)
   run.assert_called_once()
   command=run.call_args.args[0]
   self.assertNotIn('compose',command)
   self.assertEqual(command[command.index('--network')+1],'none')
   self.assertNotIn('--publish',command)
   self.assertEqual(command[command.index('--cap-drop')+1],'ALL')
   self.assertEqual(command[command.index('--cap-add')+1],'DAC_OVERRIDE')
   self.assertIn('--read-only',command)
 def test_apply_checks_before_recreate_and_refreshes_origin(self):
  with tempfile.TemporaryDirectory() as temp:
   state=self.state(temp);self.options(state)
   with patch.object(m,'package',return_value={'images':{}}),patch.object(m,'check_https',side_effect=RuntimeError('invalid certificate')),patch.object(m,'run') as run,patch('sys.argv',['install.py','apply-https','--state',str(state)]):
    with self.assertRaises(RuntimeError):m.main()
    run.assert_not_called()
   with patch.object(m,'package',return_value={'images':{}}),patch.object(m,'check_https') as check,patch.object(m,'wait_web') as ready,patch.object(m,'run') as run,patch('sys.argv',['install.py','apply-https','--state',str(state)]):
    m.main()
    check.assert_called_once();ready.assert_called_once()
    self.assertEqual(run.call_args_list[0].args[0][-1],'init-secrets')
    self.assertIn('--force-recreate',run.call_args_list[1].args[0])
if __name__=='__main__':unittest.main()
