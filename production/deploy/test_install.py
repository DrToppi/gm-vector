import importlib.util,json,os,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('vector_install',Path(__file__).with_name('install.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class Installation(unittest.TestCase):
 def test_unique_private_first_run(self):
  with tempfile.TemporaryDirectory() as temp:
   states=[Path(temp)/'one',Path(temp)/'two']
   for s in states:m.initialize(s,19443,'172.30.91.0/29')
   for name in ('encryption.key','database.password'):
    self.assertNotEqual((states[0]/'config'/name).read_bytes(),(states[1]/'config'/name).read_bytes())
   for s in states:
    self.assertEqual(s.stat().st_mode&0o777,0o700)
    for p in s.rglob('*'):
     if p.is_file():self.assertEqual(p.stat().st_mode&0o777,0o644 if p.name=='nginx.conf' else 0o600)
    config=json.loads((s/'config/config.json').read_text())
    self.assertEqual(config['origin'],'https://localhost:19443')
    self.assertEqual(config['planning_timezone'],'Europe/Moscow')
    self.assertEqual(len((s/'config/encryption.key').read_bytes()),32)
    self.assertNotIn('bootstrap_file',config)
    self.assertFalse((s/'config/bootstrap.token').exists())
    self.assertFalse((s/'setup-url.txt').exists())
   with self.assertRaises(FileExistsError):m.initialize(states[0],19443,'172.30.91.0/29')
 def test_invalid_options_no_state(self):
  with tempfile.TemporaryDirectory() as temp:
   for port,subnet in [(80,'172.30.91.0/29'),(9443,'8.8.8.0/29'),(9443,'172.30.91.0/24')]:
    s=Path(temp)/'new'
    with self.assertRaises(ValueError):m.initialize(s,port,subnet)
    self.assertFalse(s.exists())
 def test_missing_state_never_starts_services(self):
  with tempfile.TemporaryDirectory() as temp,patch.object(m,'run') as run:
   with self.assertRaises(FileNotFoundError):m.prepare(Path(temp)/'missing',{'images':{}})
   run.assert_not_called()
 def test_public_key_rejects_private_parameters(self):
  with tempfile.TemporaryDirectory() as temp:
   p=Path(temp)/'key.json';p.write_text(json.dumps({'kty':'RSA','d':'must-not-be-copied'}))
   with self.assertRaises(ValueError):m.report_public_key(p)
 def test_recovery_transport_is_exact_origin_and_private(self):
  with tempfile.TemporaryDirectory() as temp:
   state=Path(temp)/'state';m.initialize(state,19443,'172.30.91.0/29');target=Path(temp)/'recovery';target.mkdir(mode=0o700)
   token='a'*43;link='https://localhost:19443/#setup='+token
   for invalid in ['http://localhost:19443/#setup='+token,'https://other.invalid/#setup='+token,link+'&extra=1',link+'\n',link.replace('/#','/path#')]:
    with self.assertRaises(ValueError):m.save_recovery_link(state,target,invalid)
    self.assertFalse((target/'recovery-url.txt').exists())
   m.save_recovery_link(state,target,link)
   self.assertEqual((target/'recovery-url.txt').read_text(),link)
   self.assertEqual((target/'recovery-url.txt').stat().st_mode&0o777,0o600)
   with self.assertRaises(FileExistsError):m.save_recovery_link(state,target,link)
if __name__=='__main__':unittest.main()
