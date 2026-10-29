"""Explicit host-updater configuration, using private synthetic state/home only."""
import contextlib
import io
import json
import os
from pathlib import Path
import runpy
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))


class ConfigureUpdaterTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.state = self.root / 'state'
        self.state.mkdir(mode=0o700)
        (self.state / 'config').mkdir(mode=0o700)
        (self.state / 'config/config.json').write_text('{"origin":"https://synthetic.invalid"}')
        (self.state / 'deployment.json').write_text(json.dumps({'commit': 'a' * 40}))
        (self.state / 'installation.json').write_text('{"project":"gm-vector-synthetic"}')
        self.home = self.root / 'synthetic-home'
        self.home.mkdir(mode=0o700)

    def tearDown(self):
        self.temp.cleanup()

    def configure(self):
        output = io.StringIO()
        argv = ['configure-updater.py', '--state', str(self.state), '--source', str(ROOT.parents[1])]
        with patch('sys.argv', argv), patch('pathlib.Path.home', return_value=self.home), patch('subprocess.check_output', side_effect=['', 'a' * 40]), contextlib.redirect_stdout(output):
            runpy.run_path(str(ROOT / 'configure-updater.py'), run_name='__main__')
        return output.getvalue()

    def test_umask_077_produces_accessible_shared_directory_and_private_credentials(self):
        original = os.umask(0o077)
        try:
            output = self.configure()
        finally:
            os.umask(original)
        shared = self.state / 'updater/shared'
        self.assertEqual(shared.stat().st_mode & 0o777, 0o755)
        credential = self.state / 'config/updater.token'
        self.assertEqual(credential.stat().st_mode & 0o777, 0o600)
        self.assertNotIn(credential.read_text(), output)
        self.assertEqual((self.state / 'updater.compose.yaml').stat().st_mode & 0o777, 0o600)
        overlay = (self.state / 'updater.compose.yaml').read_text()
        self.assertIn('/run/updater:ro', overlay)
        self.assertNotIn('docker.sock', overlay)
        self.assertNotIn('worker:', overlay)
        unit = self.home / '.config/systemd/user/gm-vector-updater-gm-vector-synthetic.service'
        self.assertEqual(unit.stat().st_mode & 0o777, 0o600)
        self.assertIn('UMask=0077', unit.read_text())
        self.assertIn('NoNewPrivileges=true', unit.read_text())
        before = credential.read_bytes()
        self.configure()
        self.assertEqual(credential.read_bytes(), before)

    def test_symlink_shared_directory_is_rejected_before_credentials_are_created(self):
        (self.state / 'updater').mkdir(mode=0o700)
        target = self.root / 'outside'; target.mkdir()
        (self.state / 'updater/shared').symlink_to(target, target_is_directory=True)
        with self.assertRaises(SystemExit):
            self.configure()
        self.assertFalse((self.state / 'config/updater.token').exists())
        self.assertEqual(list(target.iterdir()), [])


if __name__ == '__main__':
    unittest.main()
