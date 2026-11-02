"""Bounded protocol and failure cleanup units; no Docker or user configuration."""
import importlib.util
import contextlib
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
    def test_dump_failure_targets_only_its_owned_backend(self):
        @contextlib.contextmanager
        def snapshot(*args):
            yield '00000003-00000022-1', 'synthetic-counts'
        with patch.object(b, 'database_snapshot', snapshot), patch.object(b, 'run', side_effect=subprocess.TimeoutExpired(['synthetic'], 180)) as run, patch.object(b, 'terminate_owned_backend') as cleanup:
            with self.assertRaises(subprocess.TimeoutExpired):
                b.capture_database('synthetic-container')
        cleanup.assert_called_once()
        container, operation = cleanup.call_args.args
        self.assertEqual(container, 'synthetic-container')
        self.assertRegex(operation, r'^gm-vector-backup-dump-[0-9a-f]{24}$')
        self.assertIn('PGAPPNAME=' + operation, run.call_args.args[0])

    def test_remote_cleanup_rejects_unowned_identity_and_failure(self):
        with patch.object(b.subprocess, 'run') as run:
            for invalid in ('pg_dump', '', 'gm-vector-backup-dump-unsafe;sql'):
                with self.assertRaises(ValueError):
                    b.terminate_owned_backend('synthetic', invalid)
            run.assert_not_called()
            operation = 'gm-vector-backup-dump-' + 'a' * 24
            for output in (b'f\n', b'unknown\n', b't' * 5000):
                run.return_value = types.SimpleNamespace(returncode=0, stdout=output)
                with self.assertRaisesRegex(RuntimeError, 'cleanup failed'):
                    b.terminate_owned_backend('synthetic', operation)
            run.return_value = types.SimpleNamespace(returncode=0, stdout=b't\n')
            b.terminate_owned_backend('synthetic', operation)
            sql = run.call_args.args[0][-1]
            self.assertIn("datname='planner' AND usename='planner'", sql)
            self.assertIn("application_name='" + operation + "'", sql)
    def test_exported_snapshot_protocol_has_exact_members(self):
        payload = 'SNAPSHOT|00000003-00000022-1\n' + '\n'.join(n + '|0' for n in b.COUNT_TABLES) + '\nREADY\n'
        variants = [payload.replace('users|0', 'users|invalid'), payload.replace('users|0', 'projects|0'),
                    payload.replace('00000003-00000022-1', 'unsafe;sql'), payload.replace('READY', 'UNKNOWN'),
                    payload.replace('READY', 'x' * 5000)]
        for text in [payload] + variants:
            child = subprocess.Popen([sys.executable, '-c', 'import sys;sys.stdout.write(' + repr(text) + ')'],
                                     stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            try:
                if text == payload:
                    self.assertEqual(b.snapshot_protocol(child), ('00000003-00000022-1', '\n'.join(n + '|0' for n in b.COUNT_TABLES)))
                else:
                    with self.assertRaises((ValueError, RuntimeError)):
                        b.snapshot_protocol(child)
            finally:
                child.stdout.close()
                child.wait(timeout=2)

    def test_actual_exporter_closes_on_consumer_failure(self):
        payload = 'SNAPSHOT|00000003-00000022-1\n' + '\n'.join(n + '|0' for n in b.COUNT_TABLES) + '\nREADY\n'
        program = 'import sys;sys.stdout.write(' + repr(payload) + ');sys.stdout.flush()\nfor line in sys.stdin:\n if line.startswith("ROLLBACK"):break\n'
        children = []
        original = subprocess.Popen
        def observed(*args, **kwargs):
            child = original(*args, **kwargs)
            children.append(child)
            return child
        with patch.object(b, 'DOCKER', [sys.executable, '-c', program]), patch.object(b.subprocess, 'Popen', observed):
            with self.assertRaisesRegex(RuntimeError, 'controlled'):
                with b.database_snapshot('synthetic-no-docker'):
                    raise RuntimeError('controlled')
        self.assertEqual(len(children), 1)
        self.assertIsNotNone(children[0].poll())
        self.assertTrue(children[0].stdin.closed)
        self.assertTrue(children[0].stdout.closed)
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
            with patch.object(b, 'capture_database', side_effect=subprocess.TimeoutExpired(['synthetic'], 180)):
                with self.assertRaises(subprocess.TimeoutExpired):
                    b.backup(args)
            self.assertEqual(list((root / 'archives').glob('*.gma')), [])


if __name__ == '__main__':
    unittest.main()
