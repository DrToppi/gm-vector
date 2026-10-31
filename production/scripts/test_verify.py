"""Prevent a green pipeline when a mandatory Go test did not actually pass."""
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from verify import main, require_go_test


class MandatoryTestEvidence(unittest.TestCase):
    def events(self, *events):
        return '\n'.join(json.dumps(event) for event in events)

    def test_exact_test_pass_is_accepted(self):
        require_go_test(self.events(
            {'Action': 'run', 'Test': 'TestBrowserE2E'},
            {'Action': 'pass', 'Test': 'TestBrowserE2E'},
            {'Action': 'pass', 'Package': 'planner'}), 'TestBrowserE2E')

    def test_empty_or_only_package_pass_is_rejected(self):
        for report in ('', self.events({'Action': 'pass', 'Package': 'planner'})):
            with self.subTest(report=report), self.assertRaises(RuntimeError):
                require_go_test(report, 'TestBrowserE2E')

    def test_skip_failure_or_only_subtest_pass_is_rejected(self):
        for event in (
            {'Action': 'skip', 'Test': 'TestBrowserE2E'},
            {'Action': 'fail', 'Test': 'TestBrowserE2E'},
            {'Action': 'pass', 'Test': 'TestBrowserE2E/subtest'},
            {'Action': 'pass', 'Test': 'TestOther'},
        ):
            with self.subTest(event=event), self.assertRaises(RuntimeError):
                require_go_test(self.events(event), 'TestBrowserE2E')

    def test_malformed_report_is_rejected(self):
        with self.assertRaises(json.JSONDecodeError):
            require_go_test('not JSON', 'TestBrowserE2E')

    def test_parent_pass_does_not_hide_a_skipped_required_child(self):
        with self.assertRaises(RuntimeError):
            require_go_test(self.events(
                {'Action': 'skip', 'Test': 'TestBrowserE2E/required_case'},
                {'Action': 'pass', 'Test': 'TestBrowserE2E'}), 'TestBrowserE2E')

    def test_unrelated_optional_skip_does_not_invalidate_the_named_test(self):
        require_go_test(self.events(
            {'Action': 'skip', 'Test': 'TestOptionalProfile'},
            {'Action': 'pass', 'Test': 'TestBrowserE2E'}), 'TestBrowserE2E')


class FocusedHistoryVerification(unittest.TestCase):
    def test_evidence_is_private_and_never_overwrites_an_existing_file(self):
        with tempfile.TemporaryDirectory() as directory:
            evidence = Path(directory) / 'evidence.log'
            command = [sys.executable, str(Path(__file__).with_name('verify.py')),
                       'integration', '--evidence-file', str(evidence)]
            env = {**os.environ, 'GMA_GO_BIN': 'go', 'GMA_TEST_DSN': 'postgres://localhost/planner'}
            first = subprocess.run(command, env=env, capture_output=True, timeout=10)
            self.assertEqual(first.returncode, 2)
            self.assertEqual(first.stdout, b'')
            self.assertEqual(evidence.stat().st_mode & 0o777, 0o600)
            original = evidence.read_bytes()
            self.assertIn(b'production refused', original)
            second = subprocess.run(command, env=env, capture_output=True, timeout=10)
            self.assertNotEqual(second.returncode, 0)
            self.assertEqual(evidence.read_bytes(), original)

    def test_focus_cannot_masquerade_as_all(self):
        with patch('sys.argv', ['verify.py', 'all', '--only-test', 'TestTimeHistoryExportIntegration']), \
             patch('sys.stderr', new_callable=io.StringIO), patch('subprocess.run') as run:
            with self.assertRaises(SystemExit) as stopped:
                main()
            self.assertEqual(stopped.exception.code, 2)
            run.assert_not_called()

    def test_focus_still_refuses_production_database(self):
        with patch('sys.argv', ['verify.py', 'integration', '--only-test', 'TestTimeHistoryExportIntegration']), \
             patch.dict(os.environ, {'GMA_GO_BIN': 'go', 'GMA_TEST_DSN': 'postgres://localhost/planner'}), \
             patch('sys.stderr', new_callable=io.StringIO), patch('subprocess.run') as run:
            with self.assertRaises(SystemExit) as stopped:
                main()
            self.assertEqual(stopped.exception.code, 2)
            run.assert_not_called()

    def test_focus_runs_exact_named_test_with_pass_evidence(self):
        test = 'TestTimeHistoryExportIntegration'
        report = json.dumps({'Action': 'pass', 'Test': test})
        result = subprocess.CompletedProcess([], 0, stdout=report, stderr='')
        with patch('sys.argv', ['verify.py', 'integration', '--only-test', test]), \
             patch.dict(os.environ, {'GMA_GO_BIN': 'go', 'GMA_TEST_DSN': 'postgres://localhost/gma_planner_test'}), \
             patch('sys.stdout', new_callable=io.StringIO) as output, \
             patch('subprocess.run', return_value=result) as run:
            self.assertEqual(main(), 0)
            self.assertIn('not full integration acceptance', output.getvalue())
            self.assertEqual(run.call_count, 1)
            self.assertIn('^' + test + '$', run.call_args.args[0])
            self.assertEqual(run.call_args.kwargs['env']['GMA_TEST_DSN'], 'postgres://localhost/gma_planner_test')


class FocusedBrowserVerification(unittest.TestCase):
    def test_focus_cannot_masquerade_as_complete_other_suites(self):
        for suite in ('all', 'unit', 'integration'):
            with self.subTest(suite=suite), \
                 patch('sys.argv', ['verify.py', suite, '--only-browser-api']), \
                 patch('sys.stderr', new_callable=io.StringIO), patch('subprocess.run') as run:
                with self.assertRaises(SystemExit) as stopped:
                    main()
                self.assertEqual(stopped.exception.code, 2)
                run.assert_not_called()

    def test_focus_refuses_a_real_database_before_build(self):
        with patch('sys.argv', ['verify.py', 'e2e', '--only-browser-api']), \
             patch.dict(os.environ, {'GMA_GO_BIN': 'go', 'GMA_TEST_DSN': 'postgres://localhost/planner'}), \
             patch('sys.stderr', new_callable=io.StringIO), patch('subprocess.run') as run:
            with self.assertRaises(SystemExit) as stopped:
                main()
            self.assertEqual(stopped.exception.code, 2)
            run.assert_not_called()

    def test_focus_builds_and_requires_actual_named_browser_pass(self):
        report = json.dumps({'Action': 'pass', 'Test': 'TestBrowserE2E'})
        result = subprocess.CompletedProcess([], 0, stdout=report, stderr='')
        with patch('sys.argv', ['verify.py', 'e2e', '--only-browser-api']), \
             patch.dict(os.environ, {'GMA_GO_BIN': 'go', 'GMA_TEST_DSN': 'postgres://localhost/gma_planner_test'}), \
             patch('sys.stdout', new_callable=io.StringIO) as output, \
             patch('subprocess.run', return_value=result) as run:
            self.assertEqual(main(), 0)
            self.assertIn('not full E2E acceptance', output.getvalue())
            self.assertEqual(run.call_count, 2)
            self.assertEqual(run.call_args_list[0].args[0], ['npm', 'run', 'build'])
            self.assertIn('^TestBrowserE2E$', run.call_args.args[0])
            self.assertEqual(run.call_args.kwargs['env']['GMA_E2E'], '1')
            self.assertEqual(run.call_args.kwargs['env']['GMA_TEST_DSN'], 'postgres://localhost/gma_planner_test')
