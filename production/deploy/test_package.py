import importlib.util,json,os,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
from types import SimpleNamespace
spec=importlib.util.spec_from_file_location('demo_package',Path(__file__).with_name('package.py'));m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class DistributionBoundary(unittest.TestCase):
 def test_notice_symlinks_and_special_files_rejected_before_copy(self):
  with tempfile.TemporaryDirectory() as temp:
   root=Path(temp);notices=root/'notices';notices.mkdir();(notices/'public.txt').write_text('notice')
   m.regular_source(notices,tree=True)
   secret=root/'private';secret.write_text('SYNTHETIC-PRIVATE')
   link=notices/'linked';link.symlink_to(secret)
   with self.assertRaises(ValueError):m.regular_source(notices,tree=True)
   link.unlink();os.mkfifo(link)
   with self.assertRaises(ValueError):m.regular_source(notices,tree=True)
   link.unlink();alias=root/'alias';alias.symlink_to(notices,target_is_directory=True)
   with self.assertRaises(ValueError):m.regular_source(alias/'public.txt')
