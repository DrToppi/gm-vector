"""Synthetic updater engine state, ordering and source boundary checks."""
import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import update_engine as updater

SOURCE = Path(__file__).resolve().parents[2]
SHA = 'a' * 40
IMAGES = {name: 'sha256:' + letter * 64 for name, letter in [('app', 'a'), ('db', 'b'), ('proxy', 'c')]}


class UpdateEngineTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.state = Path(self.temp.name) / 'state'
        self.state.mkdir(mode=0o700)
        self.deployment = {'commit': SHA, 'images': IMAGES, 'origin': 'https://synthetic.invalid'}
        updater.atomic(self.state / 'deployment.json', self.deployment)
        self.engines = []

    def tearDown(self):
        for engine in self.engines:
            os.close(engine.lock)
        self.temp.cleanup()

    def engine(self):
        value = updater.Engine(self.state, SOURCE)
        self.engines.append(value)
        return value

    def release(self, engine):
        os.close(engine.lock)
        self.engines.remove(engine)

    def test_git_reference_injection_is_rejected(self):
        for value in ('--upload-pack=evil', '-v', '../main', 'a..b', 'a//b', 'x.lock',
                      'x/', 'x.', 'a b', 'main;touch x', '$(echo x)', 'a\nmain', '', None,
                      'https://evil.invalid/ref', 'a' * 122):
            with self.subTest(value=value):
                with self.assertRaises(ValueError): updater.reference(value)
        for value in ('main', SHA, 'v0.2.0', 'refs/tags/v0.2.0', 'feature/safe-name'):
            self.assertEqual(updater.reference(value), value)

    def test_checkout_uses_fixed_remote_and_argument_arrays(self):
        engine = self.engine()
        calls = []
        def command(args, **kwargs):
            calls.append((args, kwargs))
            if 'ls-remote' in args: return SHA + '\trefs/tags/v0.2.0'
            return SHA if 'rev-parse' in args else ''
        engine.command = command
        path, actual = engine.checkout('refs/tags/v0.2.0')
        self.assertEqual(actual, SHA)
        fetch = next(args for args, kwargs in calls if 'fetch' in args)
        self.assertEqual(fetch[-2:], [updater.REPOSITORY, 'refs/tags/v0.2.0'])
        self.assertIn('--no-recurse-submodules', fetch)
        self.assertTrue(path.is_relative_to(engine.root / 'releases'))
        self.assertTrue(all(kwargs['env']['GIT_TERMINAL_PROMPT'] == '0' for args, kwargs in calls))

    def test_unadvertised_fork_sha_is_not_fetched(self):
        engine = self.engine()
        calls = []
        def command(args, **kwargs):
            calls.append(args)
            return SHA + '\trefs/heads/main' if 'ls-remote' in args else ''
        engine.command = command
        with self.assertRaisesRegex(ValueError, 'unadvertised_source'):
            engine.checkout('f' * 40)
        self.assertFalse(any('fetch' in args for args in calls))

    def test_moved_advertised_ref_is_rejected_before_checkout(self):
        engine = self.engine()
        calls = []
        def command(args, **kwargs):
            calls.append(args)
            if 'ls-remote' in args: return SHA + '\trefs/heads/main'
            if 'rev-parse' in args: return 'f' * 40
            return ''
        engine.command = command
        with self.assertRaises(ValueError): engine.checkout('main')
        self.assertFalse(any('checkout' in args for args in calls))

    def test_raw_sha_is_accepted_only_when_advertised_as_official_tip(self):
        engine = self.engine()
        calls = []
        def command(args, **kwargs):
            calls.append(args)
            if 'ls-remote' in args: return SHA + '\trefs/heads/main'
            if 'rev-parse' in args: return SHA
            return ''
        engine.command = command
        _, actual = engine.checkout(SHA)
        self.assertEqual(actual, SHA)
        fetch = next(args for args in calls if 'fetch' in args)
        self.assertEqual(fetch[-2:], [updater.REPOSITORY, 'refs/heads/main'])

    def test_startup_interrupted_maintenance_positively_stops_writers(self):
        (self.state / 'updater').mkdir(mode=0o700)
        updater.atomic(self.state / 'updater/operation.json',
                       {'state': 'updating', 'phase': 'verify', 'writers_stopped': True})
        with patch.object(updater.Engine, 'stop_writers') as stop:
            engine = self.engine()
        stop.assert_called_once_with()
        self.assertEqual(engine.status()['state'], 'recovery_required')
        self.assertTrue(engine.status()['recovery_available'])
        self.assertFalse(engine.record.get('stop_unconfirmed', False))

    def test_annotated_tag_uses_peeled_commit_for_name_and_raw_sha(self):
        engine = self.engine()
        tag_object = 'b' * 40
        advertised = tag_object + '\trefs/tags/v0.3.0\n' + SHA + '\trefs/tags/v0.3.0^{}'
        for ref in ('v0.3.0', SHA):
            with self.subTest(ref=ref):
                calls = []
                def command(args, **kwargs):
                    calls.append(args)
                    if 'ls-remote' in args: return advertised
                    if 'rev-parse' in args: return SHA
                    return ''
                engine.command = command
                _, actual = engine.checkout(ref)
                self.assertEqual(actual, SHA)
                self.assertNotEqual(actual, tag_object)
                fetch = next(args for args in calls if 'fetch' in args)
                self.assertEqual(fetch[-2:], [updater.REPOSITORY, 'refs/tags/v0.3.0'])
                checkout = next(args for args in calls if 'checkout' in args)
                self.assertEqual(checkout[-1], SHA)

    def test_private_state_symlink_and_permissions_rejected(self):
        link = Path(self.temp.name) / 'link'
        link.symlink_to(self.state, target_is_directory=True)
        with self.assertRaises(ValueError): updater.private_tree(link)
        self.state.chmod(0o755)
        with self.assertRaises(ValueError): updater.private_tree(self.state)
        self.state.chmod(0o700)

    def test_subprocess_errors_do_not_expose_stderr_or_stdout(self):
        engine = self.engine()
        failure = subprocess.CompletedProcess([], 1, b'SYNTHETIC_PRIVATE_STDOUT', b'SYNTHETIC_PRIVATE_STDERR')
        captured = io.StringIO()
        with patch.object(updater.subprocess, 'run', return_value=failure), contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
            with self.assertRaisesRegex(RuntimeError, '^operation_failed$'):
                engine.command(['synthetic-command'])
        self.assertEqual(captured.getvalue(), '')

    def test_lock_excludes_second_process_engine(self):
        self.engine()
        with self.assertRaises(BlockingIOError): updater.Engine(self.state, SOURCE)

    def test_interrupted_journal_is_not_resumed_automatically(self):
        for stopped, expected in ((False, 'failed'), (True, 'recovery_required')):
            (self.state / 'updater').mkdir(mode=0o700, exist_ok=True)
            updater.atomic(self.state / 'updater' / 'operation.json', {'state': 'updating', 'phase': 'migrate', 'writers_stopped': stopped})
            engine = self.engine()
            self.assertEqual(engine.status()['state'], expected)
            self.assertEqual(json.loads((engine.root / 'operation.json').read_text())['state'], expected)
            self.release(engine)

    def test_running_operation_rejects_concurrent_submission(self):
        engine = self.engine()
        entered, release = threading.Event(), threading.Event()
        def check():
            entered.set()
            release.wait(3)
        engine.check = check
        first = engine.submit('check', {'ref': 'main'})
        self.assertTrue(entered.wait(2))
        self.assertEqual(first['state'], 'checking')
        try:
            with self.assertRaises(BlockingIOError): engine.submit('check', {'ref': 'v0.2.0'})
        finally:
            release.set()
        for _ in range(100):
            if not engine.running: break
            threading.Event().wait(.01)
        self.assertFalse(engine.running)

    def test_apply_requires_checked_candidate_and_explicit_sha_confirmation(self):
        engine = self.engine()
        engine.record = {'state': 'ready', 'commit': SHA, 'phase': 'complete'}
        for data in ({'commit': 'b' * 40, 'confirmation': SHA}, {'commit': SHA, 'confirmation': ''}):
            with self.assertRaises(BlockingIOError): engine.submit('apply', data)
        self.assertEqual(engine.record['state'], 'ready')

    def test_recovery_required_blocks_new_check_and_mismatched_recovery(self):
        engine = self.engine()
        engine.record = {'state': 'recovery_required', 'id': 'd' * 32, 'writers_stopped': True}
        with self.assertRaises(BlockingIOError): engine.submit('check', {'ref': 'main'})
        with self.assertRaises(BlockingIOError):
            engine.submit('recover', {'operation_id': 'e' * 32, 'confirmation': 'e' * 32})
        self.assertEqual(engine.record['state'], 'recovery_required')

    def test_status_does_not_publish_internal_paths_or_diagnostics(self):
        engine = self.engine()
        engine.record = {'state': 'failed', 'phase': 'build', 'source': '/private/fixture',
                         'error': 'SYNTHETIC_PRIVATE_DIAGNOSTIC', 'backup': '/private/archive'}
        public = json.dumps(engine.status())
        self.assertNotIn('/private', public)
        self.assertNotIn('SYNTHETIC_PRIVATE_DIAGNOSTIC', public)
        engine.save()
        self.assertEqual((engine.root / 'operation.json').stat().st_mode & 0o777, 0o600)

    def test_changed_infrastructure_is_rejected(self):
        engine = self.engine()
        old, new = Path(self.temp.name) / 'old', Path(self.temp.name) / 'new'
        for folder in (old, new):
            for name in updater.CONTRACT:
                path = folder / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('same')
        engine.compatible(old, new)
        (new / updater.CONTRACT[0]).write_text('changed')
        with self.assertRaisesRegex(ValueError, 'infrastructure_change_requires_operator'): engine.compatible(old, new)

    def test_running_image_drift_is_detected_before_maintenance(self):
        engine = self.engine()
        engine.record = {'previous': self.deployment}
        installer = Mock()
        installer.docker.return_value = ['docker']
        def run(command, *args, **kwargs):
            if 'inspect' in command: return 'sha256:' + 'f' * 64
            return 'synthetic-container'
        installer.run.side_effect = run
        engine.context = Mock(return_value=(installer, ['compose'], {}))
        with self.assertRaisesRegex(ValueError, 'deployment_drift'): engine.assert_current()
        self.assertFalse(any('stop' in item.args[0] for item in installer.run.call_args_list))

    def test_recovery_retry_after_database_switch_does_not_restore_twice(self):
        engine = self.engine()
        engine.record = {'id': 'd' * 32, 'state': 'recovering', 'phase': 'restore',
                         'writers_stopped': True, 'migration_started': True, 'database': 'synthetic-db',
                         'previous': self.deployment}
        installer = Mock()
        installer.run.return_value = ''
        installer.prepare.side_effect = [RuntimeError('synthetic startup failure'), None]
        engine.context = Mock(return_value=(installer, ['compose'], {}))
        engine.archive = Mock(return_value=({'database.dump': b'synthetic dump'}, {'counts': 'projects|1'}))
        engine.backup.counts = Mock(return_value='projects|1')
        calls = []
        current_oid = ['100']
        def run(command, data=None):
            calls.append((command, data))
            if data and data.startswith(b"SELECT oid FROM pg_database WHERE datname='planner'"):
                return current_oid[0].encode()
            if data and data.startswith(b'SELECT oid FROM pg_database'):
                return b'200'
            if data and data.startswith(b'BEGIN; ALTER DATABASE'):
                current_oid[0] = '200'
            return b''
        engine.backup.run = run
        engine.execute('recover')
        self.assertEqual(engine.record['state'], 'recovery_required')
        self.assertTrue(engine.record['database_restored'])
        restores = sum('pg_restore' in command for command, data in calls)
        self.assertEqual(restores, 1)
        engine.execute('recover')
        self.assertEqual(engine.record['state'], 'succeeded')
        self.assertEqual(sum('pg_restore' in command for command, data in calls), 1)
        rename = [data for command, data in calls if data and data.startswith(b'BEGIN; ALTER DATABASE')]
        self.assertEqual(len(rename), 1)
        self.assertIn(b'COMMIT;', rename[0])
        self.assertEqual(engine.manifest(), self.deployment)

    def test_restore_count_mismatch_never_switches_live_database(self):
        engine = self.engine()
        engine.record = {'id': 'd' * 32, 'state': 'recovering', 'phase': 'restore',
                         'writers_stopped': True, 'migration_started': True, 'database': 'synthetic-db',
                         'previous': self.deployment}
        installer = Mock()
        installer.run.return_value = ''
        engine.context = Mock(return_value=(installer, ['compose'], {}))
        engine.archive = Mock(return_value=({'database.dump': b'synthetic dump'}, {'counts': 'projects|1'}))
        engine.backup.counts = Mock(return_value='projects|0')
        calls = []
        def run(command, data=None):
            calls.append((command, data))
            return b'100' if data and data.startswith(b"SELECT oid FROM pg_database WHERE datname='planner'") else b''
        engine.backup.run = run
        engine.execute('recover')
        self.assertEqual(engine.record['state'], 'recovery_required')
        self.assertFalse(engine.record.get('database_restored', False))
        self.assertFalse(any(data and b'ALTER DATABASE' in data for command, data in calls))
        installer.prepare.assert_not_called()
        self.assertEqual(engine.manifest(), self.deployment)

    def prepared_apply(self, failure):
        engine = self.engine()
        engine.record = {'id': 'd' * 32, 'state': 'updating', 'phase': 'queued', 'commit': 'e' * 40,
                         'previous': self.deployment, 'images': {**IMAGES, 'app': 'sha256:' + 'e' * 64},
                         'previous_source': str(SOURCE), 'source': str(SOURCE)}
        calls = []
        installer = Mock()
        installer.run.side_effect = lambda command, *args, **kwargs: calls.append(command) or ''
        def prepare(*args):
            calls.append('MIGRATE')
            raise RuntimeError('SYNTHETIC_MIGRATION_FAILURE')
        engine.activate = Mock(side_effect=prepare)
        engine.context = Mock(return_value=(installer, ['compose'], {}))
        engine.compatible = Mock()
        engine.assert_current = Mock()  # Preconditions are exercised separately; isolate maintenance ordering.
        def snapshot(label):
            calls.append('BACKUP')
            if failure == 'backup': raise RuntimeError('SYNTHETIC_BACKUP_FAILURE')
        engine.snapshot = snapshot
        engine.archive = Mock()
        return engine, calls, installer

    def test_backup_failure_keeps_services_stopped_and_never_migrates(self):
        engine, calls, installer = self.prepared_apply('backup')
        engine.execute('apply')
        self.assertEqual(calls[0], ['compose', 'stop', 'proxy', 'api', 'worker'])
        self.assertEqual(calls[1], ['compose', 'ps', '--status', 'running', '-q', 'api', 'worker', 'proxy'])
        self.assertIn('BACKUP', calls)
        self.assertEqual(calls[-2:], [['compose', 'stop', 'proxy', 'api', 'worker'], ['compose', 'ps', '--status', 'running', '-q', 'api', 'worker', 'proxy']])
        engine.activate.assert_not_called()
        installer.prepare.assert_not_called()
        self.assertFalse(engine.record.get('migration_started', False))
        self.assertEqual(engine.record['state'], 'recovery_required')

    def test_migration_failure_does_not_restart_old_images_automatically(self):
        engine, calls, installer = self.prepared_apply('migration')
        engine.execute('apply')
        self.assertLess(calls.index('BACKUP'), calls.index('MIGRATE'))
        self.assertTrue(engine.record['migration_started'])
        self.assertEqual(engine.record['state'], 'recovery_required')
        self.assertEqual(engine.activate.call_count, 1)
        installer.prepare.assert_not_called()
        self.assertEqual(engine.manifest(), self.deployment)


    def test_post_activation_failure_stops_exposed_services_before_recovery_status(self):
        engine, calls, installer = self.prepared_apply('post_activation')
        def activated_failure():
            calls.extend(['API_UP', 'PROXY_UP'])
            raise RuntimeError('SYNTHETIC_POST_ACTIVATION_FAILURE')
        engine.activate.side_effect = activated_failure
        engine.execute('apply')
        self.assertLess(calls.index('BACKUP'), calls.index('API_UP'))
        self.assertEqual(calls[-2:], [['compose', 'stop', 'proxy', 'api', 'worker'], ['compose', 'ps', '--status', 'running', '-q', 'api', 'worker', 'proxy']])
        self.assertEqual(engine.record['state'], 'recovery_required')
        self.assertTrue(engine.record['writers_stopped'])
        self.assertFalse(engine.record.get('stop_unconfirmed', False))
        installer.prepare.assert_not_called()
        self.assertEqual(engine.manifest(), self.deployment)

    def test_stop_failure_is_journaled_instead_of_claimed_as_confirmed(self):
        engine, calls, installer = self.prepared_apply('post_activation')
        def run(command, *args, **kwargs):
            calls.append(command)
            if 'stop' in command:
                raise RuntimeError('SYNTHETIC_STOP_FAILURE')
            return ''
        installer.run.side_effect = run
        engine.execute('apply')
        self.assertEqual(engine.record['state'], 'recovery_required')
        self.assertTrue(engine.record['stop_unconfirmed'])
        self.assertTrue(json.loads((engine.root / 'operation.json').read_text())['stop_unconfirmed'])
        engine.activate.assert_not_called()
        self.assertNotIn('BACKUP', calls)

    def test_activate_starts_worker_only_after_api_identity_and_tls_checks(self):
        engine, _, installer = self.prepared_apply('success')
        del engine.activate  # Exercise the real staged activation method.
        calls = []
        installer.docker.return_value = ['docker']
        def run(command, *args, **kwargs):
            calls.append(command)
            if command[:3] == ['compose', 'ps', '-q']:
                return 'synthetic-api'
            if 'inspect' in command:
                return engine.record['images']['app']
            if 'release-info' in command:
                return json.dumps({'git_commit': engine.record['commit']})
            if '--entrypoint' in command:
                return '{"ready":true}'
            return ''
        installer.run.side_effect = run
        installer.wait_web.side_effect = lambda *args: calls.append('TLS_VERIFIED')
        engine.activate()
        api = ['compose', 'up', '-d', '--no-deps', 'api']
        proxy = ['compose', 'up', '-d', '--no-deps', 'proxy']
        worker = ['compose', 'up', '-d', '--no-deps', 'worker']
        self.assertLess(calls.index(api), calls.index(proxy))
        self.assertLess(calls.index(proxy), calls.index('TLS_VERIFIED'))
        self.assertLess(calls.index('TLS_VERIFIED'), calls.index(worker))
        identity = next(i for i, command in enumerate(calls) if isinstance(command, list) and 'release-info' in command)
        self.assertLess(identity, calls.index(proxy))


if __name__ == '__main__':
    unittest.main()
