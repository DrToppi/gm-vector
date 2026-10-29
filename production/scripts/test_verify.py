"""Prevent a green pipeline when a mandatory Go test did not actually pass."""
import json
import unittest
from verify import require_go_test


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
