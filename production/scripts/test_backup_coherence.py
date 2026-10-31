"""Bounded protocol and failure cleanup units; no Docker or user configuration."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('coherence_units_backup', Path(__file__).with_name('backup.py'))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)


class BackupCoherenceProtocolTests(unittest.TestCase):
    def test_snapshot_protocol_is_bounded_and_strict(self):
        with patch.object(b, 'DOCKER', [sys.executable]):
            self.assertEqual(list(b.process_lines(['-c', 'print("bounded")'])), [b'bounded'])
            with self.assertRaisesRegex(ValueError, 'record exceeds limit'):
                list(b.process_lines(['-c', 'print("x"*100)'], max_line=16))
            with self.assertRaisesRegex(ValueError, 'output exceeds limit'):
                list(b.process_lines(['-c', 'print("x"*100)'], max_output=16))
            with self.assertRaisesRegex(ValueError, 'incomplete'):
                list(b.process_lines(['-c', 'import sys;sys.stdout.write("partial")']))
            with self.assertRaisesRegex(RuntimeError, 'subprocess failed'):
                list(b.process_lines(['-c', 'raise SystemExit(2)']))

    def test_snapshot_owner_closes_on_failure(self):
        children = []
        original = subprocess.Popen

        def observed(*args, **kwargs):
            child = original(*args, **kwargs)
            children.append(child)
            return child

        with patch.object(b, 'DOCKER', [sys.executable]), patch.object(b.subprocess, 'Popen', observed):
            records = b.process_lines(['-c', 'import time;print("one",flush=True);time.sleep(30)'])
            self.assertEqual(next(records), b'one')
            records.close()
        self.assertEqual(len(children), 1)
        self.assertIsNotNone(children[0].poll())
        self.assertTrue(children[0].stdout.closed)

    def test_snapshot_timeout_terminates_child(self):
        children = []
        original = subprocess.Popen

        def observed(*args, **kwargs):
            child = original(*args, **kwargs)
            children.append(child)
            return child

        with patch.object(b, 'DOCKER', [sys.executable]), patch.object(b.subprocess, 'Popen', observed):
            with self.assertRaisesRegex(TimeoutError, 'timed out'):
                list(b.process_lines(['-c', 'import time;time.sleep(30)'], timeout=.05))
        self.assertIsNotNone(children[0].poll())
        self.assertTrue(children[0].stdout.closed)

    def test_capture_failure_does_not_publish_archive(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            cfg = root / 'config'
            cfg.mkdir(mode=0o700)
            for filename in b.CORE:
                (cfg / filename).write_bytes(b'S' * 32 if filename == 'encryption.key' else b'synthetic')
            (cfg / 'config.json').write_text(json.dumps({'secret_file': '/run/secrets/encryption.key'}))
            args = types.SimpleNamespace(config=str(cfg), destination=str(root / 'archives'),
                                         key=str(root / 'backup.key'), container='synthetic-not-contacted')
            with patch.object(b, 'run', side_effect=subprocess.TimeoutExpired(['synthetic'], 180)):
                with self.assertRaises(subprocess.TimeoutExpired):
                    b.backup(args)
            self.assertEqual(list((root / 'archives').glob('*.gma')), [])


if __name__ == '__main__':
    unittest.main()
