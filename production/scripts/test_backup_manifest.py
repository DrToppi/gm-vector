"""Synthetic-only manifest regression tests; never loads user runtime configuration."""
import base64,importlib.util,json,os,sys,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('gma_backup',Path(__file__).with_name('backup.py'))
b=importlib.util.module_from_spec(spec);spec.loader.exec_module(b)

def fixture(root):
 cfg=root/'config';cfg.mkdir(mode=0o700)
 for n in b.CORE:(cfg/n).write_bytes(b'S'*32 if n=='encryption.key' else b'synthetic')
 runtime={field:'/run/secrets/'+name for field,name in b.RUNTIME_FILES.items() if field == 'secret_file'}
 (cfg/'config.json').write_text(json.dumps(runtime));return cfg,runtime
class BackupManifestTests(unittest.TestCase):
 def test_dependencies_and_no_private_keys(self):
  with tempfile.TemporaryDirectory() as d:
   cfg,r=fixture(Path(d));r.update(smtp_file='/run/secrets/smtp.json',ldap_file='/run/secrets/ldap.json',report_public_key_file='/run/secrets/report-public-key.json')
   (cfg/'config.json').write_text(json.dumps(r));(cfg/'smtp.json').write_text('{"ca_file":"/run/secrets/smtp-ca.pem"}');(cfg/'smtp-ca.pem').write_text('CA-MARKER');(cfg/'ldap.json').write_text('{"enabled":false,"ca_file":"/run/secrets/ldap-ca.pem"}');(cfg/'ldap-ca.pem').write_text('LDAP-CA');(cfg/'report-public-key.json').write_text('PUBLIC-MARKER');(cfg/'support-private.pem').write_text('PRIVATE-NOT-ALLOWED');(cfg/'signing.key').write_text('ISSUER-NOT-ALLOWED');(cfg/'migration-config.json').write_text('{"database_url":"synthetic"}')
   values=b.collect_secrets(cfg)
   for name in ('smtp.json','smtp-ca.pem','ldap.json','ldap-ca.pem','report-public-key.json','migration-config.json'):self.assertIn('secrets/'+name,values)
   self.assertNotIn(b'PRIVATE-NOT-ALLOWED',values.values());self.assertNotIn(b'ISSUER-NOT-ALLOWED',values.values())
   (cfg/'smtp-ca.pem').unlink()
   with self.assertRaises(FileNotFoundError):b.collect_secrets(cfg)
 def test_missing_core_and_unsupported_paths(self):
  with tempfile.TemporaryDirectory() as d:
   cfg,r=fixture(Path(d));r['smtp_file']='/etc/shadow';(cfg/'config.json').write_text(json.dumps(r))
   with self.assertRaises(ValueError):b.collect_secrets(cfg)
   r.pop('smtp_file');(cfg/'config.json').write_text(json.dumps(r));(cfg/'encryption.key').unlink()
   with self.assertRaises(FileNotFoundError):b.collect_secrets(cfg)
 def test_legacy_bootstrap_is_not_a_runtime_dependency(self):
  with tempfile.TemporaryDirectory() as d:
   cfg,r=fixture(Path(d))
   r['bootstrap_file']='/run/secrets/bootstrap.token';(cfg/'config.json').write_text(json.dumps(r))
   self.assertNotIn('secrets/bootstrap.token',b.collect_secrets(cfg))
   (cfg/'bootstrap.token').write_text('synthetic-legacy-value')
   self.assertNotIn('secrets/bootstrap.token',b.collect_secrets(cfg))
   v=b.collect_secrets(cfg);v['secrets/bootstrap.token']=b'synthetic-legacy-value';v['database.dump']=b'dump'
   meta={'format':2,'dump_sha256':b.hashlib.sha256(b'dump').hexdigest(),'files':{n:{'size':len(data),'sha256':b.hashlib.sha256(data).hexdigest()} for n,data in v.items()}}
   v['manifest.json']=json.dumps(meta).encode()
   self.assertTrue(b.validate_members(v,meta))
 def test_symlinks_and_size(self):
  with tempfile.TemporaryDirectory() as d:
   root=Path(d);target=root/'target';target.write_text('secret');link=root/'link';link.symlink_to(target)
   with self.assertRaises(OSError):b.read_secret(link)
   fifo=root/'fifo';os.mkfifo(fifo)
   with self.assertRaises(ValueError):b.read_secret(fifo)
   target.write_bytes(b'x'*(b.MAX_SECRET+1))
   with self.assertRaises(ValueError):b.read_secret(target)
 def test_manifest_checks(self):
  with tempfile.TemporaryDirectory() as d:
   cfg,r=fixture(Path(d));v=b.collect_secrets(cfg);v['database.dump']=b'dump';m={'format':2,'dump_sha256':b.hashlib.sha256(b'dump').hexdigest(),'files':{n:{'size':len(data),'sha256':b.hashlib.sha256(data).hexdigest()} for n,data in v.items()}};v['manifest.json']=json.dumps(m).encode()
   self.assertTrue(b.validate_members(v,m));old=dict(m,format=1);self.assertFalse(b.validate_members(v,old))
   v['secrets/encryption.key']=b'changed'
   with self.assertRaises(ValueError):b.validate_members(v,m)
   v['../escape']=b'x'
   with self.assertRaises(ValueError):b.validate_members(v,m)
if __name__=='__main__':unittest.main()
