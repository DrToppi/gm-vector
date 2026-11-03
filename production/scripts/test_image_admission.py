"""Synthetic D14 tests: no API requests, approval events or vulnerable images."""
import copy
import contextlib
import datetime as dt
import io
import json
import os
import importlib.util
import subprocess
import sys
from pathlib import Path
import tempfile
import unittest
from urllib.error import URLError
from urllib.parse import parse_qs, urlsplit

import image_admission as a
import image_gate as gate
import test_image_gate as rawfixtures

NOW = rawfixtures.NOW


def context(event='workflow_dispatch'):
    return {'repository': a.REPOSITORY, 'repository_id': a.REPOSITORY_ID, 'run_id': '101', 'run_attempt': '1',
            'candidate_sha': 'b' * 40, 'verifier_sha': 'c' * 40, 'event_name': event,
            'workflow_ref': a.REPOSITORY + '/' + a.WORKFLOW + '@refs/heads/feature/operational-hardening'}


def environment():
    return {'id': 26111380009, 'name': a.ENVIRONMENT, 'can_admins_bypass': False,
            'deployment_branch_policy': None, 'protection_rules': [{'type': 'required_reviewers',
                'prevent_self_review': False, 'reviewers': [{'type': 'User', 'reviewer': {'id': int(a.OWNER_ID)}}]}]}


def setup():
    return {'format': 'gm-vector-exception-environment-v1', 'status': 'verified', 'repository_id': a.REPOSITORY_ID,
            'environment_id': '26111380009', 'environment_name': a.ENVIRONMENT, 'reviewer_id': a.OWNER_ID,
            'api_settings_sha256': a.digest(a.canonical(a.environment_settings(environment()))),
            'bypass_disable_verification': {'method': 'api_readback', 'verified_at': a.time_string(NOW),
                                           'evidence_sha256': 'e' * 64}}


class FakeAPI:
    def __init__(self, ctx):
        self.ctx = copy.deepcopy(ctx)
        self.repository = {'id': int(a.REPOSITORY_ID), 'full_name': a.REPOSITORY, 'owner': {'id': int(a.OWNER_ID)}}
        self.run = {'id': 101, 'run_attempt': 1, 'head_sha': 'b' * 40, 'event': ctx['event_name'],
                    'repository': {'id': int(a.REPOSITORY_ID)}, 'head_repository': {'id': int(a.REPOSITORY_ID)},
                    'path': a.WORKFLOW, 'head_branch': 'feature/operational-hardening'}
        self.environment = environment()
        self.branch_policies = []
        self.records = []
        self.artifacts = {}
        self.calls = []
        self.add_artifact('201', 'sha256:' + '1' * 64)

    def add_artifact(self, aid, digest, created=NOW):
        self.artifacts[aid] = {'id': int(aid), 'digest': digest, 'expired': False, 'size_in_bytes': 1000,
            'created_at': a.time_string(created), 'expires_at': a.time_string(NOW + dt.timedelta(days=7)),
            'workflow_run': {'id': 101, 'repository_id': int(a.REPOSITORY_ID),
                'head_repository_id': self.run['head_repository']['id'], 'head_sha': self.run['head_sha']}}

    def get(self, path):
        self.calls.append(path)
        if path == '/repos/' + a.REPOSITORY: return copy.deepcopy(self.repository)
        if path.endswith('/actions/runs/101'): return copy.deepcopy(self.run)
        if '/actions/artifacts/' in path: return copy.deepcopy(self.artifacts[path.rsplit('/', 1)[1]])
        if path.endswith('/environments/' + a.ENVIRONMENT): return copy.deepcopy(self.environment)
        raise a.AdmissionError('synthetic unsupported API endpoint')

    def pages(self, path, collection=None):
        self.calls.append(path)
        if path.endswith('/approvals'): return copy.deepcopy(self.records)
        if path.endswith('/artifacts'): return copy.deepcopy(list(self.artifacts.values()))
        if path.endswith('/deployment-branch-policies'): return copy.deepcopy(self.branch_policies)
        raise a.AdmissionError('synthetic pagination failure')


class AdmissionTests(unittest.TestCase):
    def setUp(self):
        self.raw = rawfixtures.ImageGateTests('test_complete_clean_inventory_and_reordered_cdx_layers_pass')
        self.raw.setUp()
        self.addCleanup(self.raw.doCleanups)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.ctx = context()
        self.api = FakeAPI(self.ctx)

    def privacy(self):
        value = {'format': 'gm-vector-artifact-privacy-v1', 'status': 'PASS', 'source_tree_clean': True,
                 'source_revision': self.raw.manifest['source_revision'],
                 'scanner': {'gitleaks_version': '8.30.1', 'binary_sha256': '88f91962aa2f93ac6ab281d553b9e125f5197bbbce38f9f2437f7299c32e5509'},
                 'scan_manifest_sha256': gate.file_hash(self.raw.root / 'manifest.json'),
                 'image_ids': {name: self.raw.manifest['images'][name]['image_id'] for name in a.LABELS},
                 'roots': [{'id': 'P0' + str(n), 'status': 'PASS', 'members': 1, 'body_bytes': 10,
                            'inventory_sha256': 'a' * 64, 'content_sha256': 'b' * 64, 'scanner_report_sha256': 'd' * 64}
                           for n in range(1, 7)], 'build': {name: 'f' * 64 for name in a.BUILD_KEYS}}
        path = self.root / 'privacy.json'
        path.write_bytes(a.canonical(value) + b'\n')
        return path

    def request(self):
        return {'format': 'gm-vector-image-exception-request-v1', 'requests': [{
            'role': 'app', 'advisory_id': 'TEST-ADVISORY-1', 'package': 'os-library', 'version': '1', 'target': 'debian',
            'reason': 'Await the upstream correction.', 'compensating_controls': 'Isolated reference installation.',
            'expires_at': a.time_string(NOW + dt.timedelta(days=14)), 'recheck_at': a.time_string(NOW + dt.timedelta(days=1))}]}

    def blocked_report(self, duplicate=False, status='will_not_fix'):
        def change(report):
            row = {'VulnerabilityID': 'TEST-ADVISORY-1', 'PkgID': 'os-library@1', 'PkgName': 'os-library',
                   'PkgPath': 'usr/lib/os-library', 'InstalledVersion': '1', 'FixedVersion': '',
                   'Severity': 'HIGH', 'Status': status}
            report['Results'][0]['Vulnerabilities'] = [row, copy.deepcopy(row)] if duplicate else [row]
        self.raw.change_report(change)

    def prepare(self, request=None):
        output = self.root / ('bundle-' + str(len(list(self.root.glob('bundle-*')))))
        result = a.prepare(self.raw.root, self.privacy(), output, request, context=self.ctx, now=NOW)
        return output, result

    def reviewed(self):
        self.blocked_report()
        bundle, _ = self.prepare(self.request())
        attached = a.bind(bundle, '201', '1' * 64, context=self.ctx, api=self.api, now=NOW)
        self.api.records = [{'state': 'approved', 'comment': attached['review_comment'], 'user': {'id': int(a.OWNER_ID)},
                             'environments': [{'id': 26111380009, 'name': a.ENVIRONMENT}]}]
        return bundle, attached

    def test_zero_request_strict_path_never_reads_environment_or_reviews(self):
        bundle, result = self.prepare()
        self.assertEqual(result['status'], 'PASS')
        actual = a.final_result(bundle, '201', '1' * 64, 'success', 'skipped', context=self.ctx, api=self.api, now=NOW)
        self.assertEqual(actual['status'], 'PASS')
        self.assertFalse(any('/environments/' in x or x.endswith('/approvals') for x in self.api.calls))
        self.assertEqual(sorted(p.name for p in bundle.iterdir()), list(a.FILES))

    def test_status_nulls_and_duplicate_rows_preserved_exactly(self):
        self.blocked_report(duplicate=True)
        bundle, result = self.prepare(self.request())
        self.assertEqual(result['strict_status'], 'BLOCKED')
        projected = a.parse((bundle / 'app.report.json').read_bytes())
        rows = projected['results'][0]['vulnerabilities']
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0], rows[1])
        self.assertEqual(rows[0]['status'], 'will_not_fix')
        self.assertEqual(rows[0]['fixed_version'], '')
        self.assertIsNone(projected['results'][0]['packages'][0]['id'])
        resolved = a.parse((bundle / 'waivers.json').read_bytes())['requests'][0]
        self.assertEqual(resolved['matched_count'], 2)
        self.assertEqual(resolved['reason'], self.request()['requests'][0]['reason'])

    def test_explicit_owner_review_and_post_upload_receipt_final(self):
        bundle, attached = self.reviewed()
        receipt = a.approve(bundle, '201', '1' * 64, setup(), context=self.ctx, api=self.api, now=NOW + dt.timedelta(minutes=1))
        self.assertEqual(receipt['status'], 'ADMITTED_WITH_EXCEPTIONS')
        self.assertEqual(receipt['strict_status'], 'BLOCKED')
        output = self.root / 'admission.json'; a.write_file(output, receipt)
        self.api.add_artifact('202', 'sha256:' + '2' * 64, NOW + dt.timedelta(minutes=1))
        final = a.final_result(bundle, '201', '1' * 64, 'success', 'success', context=self.ctx, api=self.api,
            now=NOW + dt.timedelta(minutes=2), admission_file=output, admission_artifact_id='202',
            admission_artifact_digest='2' * 64, environment_config=setup())
        self.assertEqual(final['status'], 'ADMITTED_WITH_EXCEPTIONS')
        self.assertEqual(final['approval_digest'], attached['approval_digest'])

    def test_missing_wrong_owner_environment_comment_rejection_never_admits(self):
        bundle, _ = self.reviewed()
        original = copy.deepcopy(self.api.records)
        variants = [[], [{**original[0], 'user': {'id': 1}}], [{**original[0], 'comment': 'approved_by owner'}],
                    [{**original[0], 'state': 'rejected'}], [{**original[0], 'environments': [{'id': 1, 'name': a.ENVIRONMENT}]}],
                    original + copy.deepcopy(original)]
        for index, records in enumerate(variants):
            with self.subTest(case=index):
                self.api.records = records
                with self.assertRaises(a.AdmissionError):
                    a.approve(bundle, '201', '1' * 64, setup(), context=self.ctx, api=self.api, now=NOW)

    def test_pending_setup_and_all_nonliteral_false_bypass_values_fail(self):
        bundle, _ = self.reviewed()
        config = setup(); config['status'] = 'pending_api_verification'; config['bypass_disable_verification'] = None
        with self.assertRaises(a.AdmissionError): a.approve(bundle, '201', '1' * 64, config, context=self.ctx, api=self.api, now=NOW)
        for bypass in (None, 0, '', 'false', True):
            self.api.environment = {**environment(), 'can_admins_bypass': bypass}
            with self.subTest(bypass=bypass), self.assertRaises(a.AdmissionError):
                a.approve(bundle, '201', '1' * 64, setup(), context=self.ctx, api=self.api, now=NOW)
        self.api.environment = environment(); del self.api.environment['can_admins_bypass']
        with self.assertRaises(a.AdmissionError): a.environment_settings(self.api.environment)

    def test_mutated_or_foreign_artifacts_fail_even_with_self_declared_approval(self):
        bundle, _ = self.reviewed()
        original = copy.deepcopy(self.api.artifacts['201'])
        for mutate in (lambda x: x.update(expired=True), lambda x: x.update(digest='sha256:' + '9' * 64),
                       lambda x: x['workflow_run'].update(id=999), lambda x: x['workflow_run'].update(repository_id=999),
                       lambda x: x['workflow_run'].update(head_repository_id=999),
                       lambda x: x['workflow_run'].update(head_sha='d' * 40)):
            self.api.artifacts['201'] = copy.deepcopy(original); mutate(self.api.artifacts['201'])
            with self.assertRaises(a.AdmissionError):
                a.approve(bundle, '201', '1' * 64, setup(), context=self.ctx, api=self.api, now=NOW)

    def test_rerun_context_old_nonce_and_edited_member_fail(self):
        bundle, _ = self.reviewed()
        newer = {**self.ctx, 'run_attempt': '2'}
        with self.assertRaises(a.AdmissionError): a.load_bundle(bundle, context=newer, now=NOW)
        original = (bundle / 'app.report.json').read_bytes()
        (bundle / 'app.report.json').write_bytes(original + b' ')
        with self.assertRaises(a.AdmissionError): a.load_bundle(bundle, context=self.ctx, now=NOW)
        (bundle / 'app.report.json').write_bytes(original)
        self.api.run['run_attempt'] = 2
        with self.assertRaises(a.AdmissionError): a.bind(bundle, '201', '1' * 64, context=self.ctx, api=self.api, now=NOW)

    def test_nonempty_missing_failed_skipped_cancelled_admission_fails(self):
        bundle, _ = self.reviewed()
        for result in ('skipped', 'failure', 'cancelled', 'success', ''):
            with self.subTest(result=result), self.assertRaises(a.AdmissionError):
                a.final_result(bundle, '201', '1' * 64, 'success', result, context=self.ctx, api=self.api, now=NOW)
        with self.assertRaises(a.AdmissionError):
            a.final_result(bundle, '201', '1' * 64, 'failure', 'success', context=self.ctx, api=self.api, now=NOW)

    def test_zero_request_conditional_success_or_receipt_is_not_green(self):
        bundle, _ = self.prepare()
        for result in ('failure', 'cancelled', 'success'):
            with self.assertRaises(a.AdmissionError):
                a.final_result(bundle, '201', '1' * 64, 'success', result, context=self.ctx, api=self.api, now=NOW)
        with self.assertRaises(a.AdmissionError):
            a.final_result(bundle, '201', '1' * 64, 'success', 'skipped', context=self.ctx, api=self.api, now=NOW,
                           admission_file=self.root / 'unexpected')

    def test_request_exact_scope_dates_and_privacy_rejections(self):
        self.blocked_report()
        base = self.request()
        changes = [lambda x: x.update(approved_by=a.OWNER_ID), lambda x: x.update(package='*'),
                   lambda x: x.update(version='2'), lambda x: x.update(target='other'),
                   lambda x: x.update(expires_at=a.time_string(NOW + dt.timedelta(days=14, seconds=1))),
                   lambda x: x.update(recheck_at=a.time_string(NOW)), lambda x: x.update(reason='password=synthetic-private-marker'),
                   lambda x: x.update(compensating_controls='Email private@example.test'),
                   lambda x: x.update(reason='Stored under /home/private/customer'), lambda x: x.update(reason='Endpoint10.1.2.3'),
                   lambda x: x.update(reason='Endpoint [fd00::1]')]
        for index, mutate in enumerate(changes):
            request = copy.deepcopy(base); mutate(request['requests'][0])
            with self.subTest(case=index), self.assertRaises(a.AdmissionError): self.prepare(request)
        request = copy.deepcopy(base); request['requests'].append(copy.deepcopy(request['requests'][0]))
        with self.assertRaises(a.AdmissionError): self.prepare(request)

    def test_unknown_uncovered_and_low_only_requests_block(self):
        self.blocked_report(status='affected')
        with self.assertRaises(a.AdmissionError): self.prepare()
        self.raw.change_report(lambda r: r['Results'][0]['Vulnerabilities'][0].update(Severity='UNKNOWN'))
        with self.assertRaises(a.AdmissionError): self.prepare(self.request())
        self.raw.change_report(lambda r: r['Results'][0]['Vulnerabilities'][0].update(Severity='LOW'))
        with self.assertRaises(a.AdmissionError): self.prepare(self.request())

    def test_raw_private_diagnostics_omitted_but_private_identity_rejected(self):
        marker = 'ghp_' + 'Q' * 36
        self.raw.change_report(lambda r: (r['Metadata'].update(Env=[marker]), r.update(Description=marker)))
        bundle, _ = self.prepare()
        self.assertTrue(marker.encode() not in b''.join(p.read_bytes() for p in bundle.iterdir()), 'public projection exposed a canary')
        self.raw.change_report(lambda r: r['Results'][0]['Packages'][0].update(Name=marker))
        with self.assertRaises(a.AdmissionError): self.prepare()

    def test_private_addresses_are_refused_in_all_public_identity_strings(self):
        for value in ('http://10.11.12.13/private-module', 'https://[fd12:3456::1]/package',
                      '169.254.169.254/metadata', 'http://127.0.0.1/module', 'https://[::1]/module'):
            with self.subTest(value=value), self.assertRaises(a.AdmissionError):
                a.public_string(value)
        for value in ('1.2.3.4', '1:4.5.6-rc.1+deb12u1', 'pkg:deb/debian/libexample@1.2.3?arch=amd64',
                      'pkg:golang/example.com/module@v1.2.3', 'https://example.com/module'):
            self.assertEqual(a.public_string(value), value)

    def test_raw_gate_success_cannot_publish_a_private_package_endpoint(self):
        self.raw.change_report(lambda r: r['Results'][0]['Packages'][0].update(
            Name='http://10.11.12.13/private-module'))
        self.assertEqual(self.raw.validate()['status'], 'PASS')
        with self.assertRaises(a.AdmissionError): self.prepare()

    def test_privacy_binding_and_inventory_cannot_be_self_asserted(self):
        path = self.privacy(); value = a.parse(path.read_bytes())
        for mutate in (lambda x: x.update(status='PASS', source_tree_clean=False), lambda x: x.update(scan_manifest_sha256='0' * 64),
                       lambda x: x['roots'].pop(), lambda x: x['image_ids'].update(app='sha256:' + '0' * 64),
                       lambda x: x['build'].pop('binary_sha256')):
            changed = copy.deepcopy(value); mutate(changed); path.write_bytes(a.canonical(changed) + b'\n')
            with self.assertRaises(a.AdmissionError):
                a.prepare(self.raw.root, path, self.root / 'rejected', context=self.ctx, now=NOW)

    def test_stale_db_and_expired_policy_are_rechecked_after_review(self):
        bundle, _ = self.reviewed()
        with self.assertRaises(a.AdmissionError):
            a.approve(bundle, '201', '1' * 64, setup(), context=self.ctx, api=self.api, now=NOW + dt.timedelta(days=1))

    def test_strict_pr_merge_candidate_and_foreign_head_compatible_without_exceptions(self):
        self.ctx = context('pull_request'); self.api = FakeAPI(self.ctx)
        self.api.run['head_sha'] = 'd' * 40; self.api.run['head_repository']['id'] = 999
        self.api.add_artifact('201', 'sha256:' + '1' * 64)
        bundle, _ = self.prepare()
        self.assertEqual(a.final_result(bundle, '201', '1' * 64, 'success', 'skipped', context=self.ctx, api=self.api, now=NOW)['status'], 'PASS')
        self.blocked_report()
        with self.assertRaises(a.AdmissionError): self.prepare(self.request())

    def test_canonical_duplicate_extra_symlink_and_request_budget(self):
        for raw in (b'{"a":1,"a":2}', b'NaN', b'Infinity', b'[' * 1100):
            with self.assertRaises(a.AdmissionError): a.parse(raw)
        with self.assertRaises(a.AdmissionError): a.request_rows({'format': 'x', 'requests': ['x' * (a.REQUEST_LIMIT + 1)]})
        bundle, _ = self.prepare()
        (bundle / 'extra.json').write_text('{}')
        with self.assertRaises(a.AdmissionError): a.load_bundle(bundle, context=self.ctx, now=NOW)
        (bundle / 'extra.json').unlink(); original = (bundle / 'waivers.json').read_bytes(); (bundle / 'waivers.json').unlink()
        target = self.root / 'target'; target.write_bytes(original); (bundle / 'waivers.json').symlink_to(target)
        with self.assertRaises(a.AdmissionError): a.load_bundle(bundle, context=self.ctx, now=NOW)

    def test_edited_nonce_unknown_resolved_fields_and_current_source_fail(self):
        bundle, _ = self.reviewed()
        manifest = a.parse((bundle / 'manifest.json').read_bytes())
        manifest['nonce'] = '0' * 64
        (bundle / 'manifest.json').write_bytes(a.canonical(manifest) + b'\n')
        with self.assertRaises(a.AdmissionError):
            a.approve(bundle, '201', '1' * 64, setup(), context=self.ctx, api=self.api, now=NOW)
        for field in ('candidate_sha', 'verifier_sha'):
            with self.assertRaises(a.AdmissionError):
                a.load_bundle(bundle, context={**self.ctx, field: 'd' * 40}, now=NOW)

    def test_live_repository_run_workflow_and_setup_cannot_be_replaced(self):
        bundle, _ = self.reviewed()
        original = copy.deepcopy(self.api)
        changes = [lambda x: x.repository['owner'].update(id=1), lambda x: x.run.update(path='other.yml'),
                   lambda x: x.run['head_repository'].update(id=999), lambda x: x.run.update(head_sha='e' * 40),
                   lambda x: x.run.update(event='pull_request'), lambda x: x.environment.update(id=999),
                   lambda x: x.environment['protection_rules'][0]['reviewers'][0]['reviewer'].update(id=1)]
        for index, mutate in enumerate(changes):
            api = copy.deepcopy(original); mutate(api)
            with self.subTest(case=index), self.assertRaises(a.AdmissionError):
                a.approve(bundle, '201', '1' * 64, setup(), context=self.ctx, api=api, now=NOW)

    def test_recheck_expiry_is_independent_of_database_staleness(self):
        self.blocked_report()
        request = self.request(); request['requests'][0]['recheck_at'] = a.time_string(NOW + dt.timedelta(minutes=1))
        bundle, _ = self.prepare(request)
        with self.assertRaises(a.AdmissionError): a.load_bundle(bundle, context=self.ctx, now=NOW + dt.timedelta(minutes=2))

    def test_verbatim_unicode_reason_and_raw_status_privacy(self):
        self.blocked_report()
        request = self.request(); request['requests'][0]['reason'] = '  Prepare correction: проверить исправление.\nRecheck.  '
        bundle, _ = self.prepare(request)
        self.assertEqual(a.parse((bundle / 'waivers.json').read_bytes())['requests'][0]['reason'], request['requests'][0]['reason'])
        self.blocked_report(status='ghp_' + 'Q' * 36)
        with self.assertRaises(a.AdmissionError): self.prepare(request)

    def test_absent_status_is_null_and_custom_branch_policy_is_live_bound(self):
        self.blocked_report()
        self.raw.change_report(lambda r: r['Results'][0]['Vulnerabilities'][0].pop('Status'))
        bundle, _ = self.prepare(self.request())
        projected = a.parse((bundle / 'app.report.json').read_bytes())
        self.assertIsNone(projected['results'][0]['vulnerabilities'][0]['status'])
        bound = a.bind(bundle, '201', '1' * 64, context=self.ctx, api=self.api, now=NOW)
        self.api.records = [{'state': 'approved', 'comment': bound['review_comment'], 'user': {'id': int(a.OWNER_ID)},
                             'environments': [{'id': 26111380009, 'name': a.ENVIRONMENT}]}]
        self.api.environment['deployment_branch_policy'] = {'protected_branches': False, 'custom_branch_policies': True}
        self.api.branch_policies = [{'id': 123, 'name': 'feature/*', 'type': 'branch'}]
        config = setup()
        config['api_settings_sha256'] = a.digest(a.canonical(a.environment_settings(self.api.environment, self.api.branch_policies)))
        a.approve(bundle, '201', '1' * 64, config, context=self.ctx, api=self.api, now=NOW)
        self.api.branch_policies[0]['name'] = 'other/*'
        with self.assertRaises(a.AdmissionError):
            a.approve(bundle, '201', '1' * 64, config, context=self.ctx, api=self.api, now=NOW)

    def test_component_and_report_group_budgets(self):
        report, sbom = rawfixtures.report_fixture('app', self.raw.manifest['images']['app'])
        template = report['Results'][0]
        report['Results'] = [{**template, 'Target': 'target-' + str(n)} for n in range(17)]
        with self.assertRaises(a.AdmissionError): a.report_projection('app', self.raw.manifest['images']['app']['image_id'], report)
        sbom['components'] = [{'name': 'x'}] * (a.ROWS_LIMIT + 1)
        with self.assertRaises(a.AdmissionError): a.inventory_projection('app', self.raw.manifest['images']['app']['image_id'], sbom)

    def test_cli_rejects_explicit_json_null_before_prepare_and_hides_arguments(self):
        spec = importlib.util.spec_from_file_location('synthetic_admission_cli', Path(__file__).with_name('image-admission.py'))
        cli = importlib.util.module_from_spec(spec); spec.loader.exec_module(cli)
        path = self.root / 'request.json'; path.write_text('null')
        from unittest.mock import patch
        with (patch.object(sys, 'argv', ['image-admission.py', 'prepare', '--scan-dir', str(self.raw.root),
                '--privacy-receipt', str(self.privacy()), '--output-dir', str(self.root / 'unused'), '--request-file', str(path)]),
                patch.object(cli.admission, 'runner_context', return_value=self.ctx), patch.object(cli.admission, 'prepare') as prepare):
            with self.assertRaises(a.AdmissionError): cli.main()
            prepare.assert_not_called()
        marker = 'ghp_' + 'Q' * 36
        result = subprocess.run([sys.executable, str(Path(__file__).with_name('image-admission.py')), '--bad-' + marker],
                                capture_output=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(marker.encode() not in result.stdout + result.stderr, 'CLI argument error exposed a canary')


class APITransportTests(unittest.TestCase):
    class Response:
        def __init__(self, value, link=''):
            self.status = 200; self.raw = value if type(value) is bytes else a.canonical(value)
            self.headers = {'Link': link}
        def __enter__(self): return self
        def __exit__(self, *args): pass
        def read(self, size): return self.raw[:size]

    def test_read_only_transport_limits_and_static_errors(self):
        class Opener:
            def __init__(inner): inner.calls = []; inner.response = self.Response({})
            def open(inner, request, timeout):
                inner.calls.append((request, timeout)); return inner.response
        opener = Opener(); api = a.GitHubAPI(env={'GITHUB_TOKEN': 'synthetic-only'}, opener=opener)
        api.get('/repos/' + a.REPOSITORY)
        self.assertEqual(opener.calls[0][0].get_method(), 'GET'); self.assertEqual(opener.calls[0][1], 10)
        for path in ('https://other.example.test', '/repos/foreign/other', '/repos/' + a.REPOSITORY + '/actions/runs/101/cancel'):
            with self.assertRaises(a.AdmissionError): api.get(path)
        opener.response = self.Response(b'x' * (a.BUNDLE_LIMIT + 1))
        with self.assertRaises(a.AdmissionError): api.get('/repos/' + a.REPOSITORY)
        class Failed:
            def open(self, *args, **kwargs): raise URLError('private marker must not escape')
        failed = a.GitHubAPI(env={'GITHUB_TOKEN': 'synthetic-only'}, opener=Failed())
        with self.assertRaises(a.AdmissionError) as error: failed.get('/repos/' + a.REPOSITORY)
        self.assertTrue('private marker' not in str(error.exception), 'API error exposed a canary')

    def test_pagination_bounds_and_foreign_links(self):
        class Opener:
            def open(inner, request, **kwargs):
                page = int(parse_qs(urlsplit(request.full_url).query)['page'][0])
                return self.Response([{}] * 100, '<https://api.github.com/repos/' + a.REPOSITORY + '/actions/runs/101/approvals?per_page=100&page=' + str(page + 1) + '>; rel="next"')
        api = a.GitHubAPI(env={'GITHUB_TOKEN': 'synthetic-only'}, opener=Opener())
        with self.assertRaisesRegex(a.AdmissionError, 'pagination exceeds'): api.pages('/repos/' + a.REPOSITORY + '/actions/runs/101/approvals')
        class Foreign:
            def open(inner, *args, **kwargs): return self.Response([], '<https://other.example.test/page>; rel="next"')
        api = a.GitHubAPI(env={'GITHUB_TOKEN': 'synthetic-only'}, opener=Foreign())
        with self.assertRaises(a.AdmissionError): api.pages('/repos/' + a.REPOSITORY + '/actions/runs/101/approvals')

    def test_complete_full_page_and_numeric_repository_link(self):
        class Full:
            def open(inner, *args, **kwargs): return self.Response([{}] * 100)
        api = a.GitHubAPI(env={'GITHUB_TOKEN': 'synthetic-only'}, opener=Full())
        self.assertEqual(len(api.pages('/repos/' + a.REPOSITORY + '/actions/runs/101/approvals')), 100)
        class Numeric:
            def open(inner, request, **kwargs):
                page = int(parse_qs(urlsplit(request.full_url).query)['page'][0])
                link = '<https://api.github.com/repositories/' + a.REPOSITORY_ID + '/actions/runs/101/approvals?per_page=100&page=2>; rel="next"' if page == 1 else ''
                return self.Response([{}] * 100 if page == 1 else [{}], link)
        api = a.GitHubAPI(env={'GITHUB_TOKEN': 'synthetic-only'}, opener=Numeric())
        self.assertEqual(len(api.pages('/repos/' + a.REPOSITORY + '/actions/runs/101/approvals')), 101)

    def test_actual_child_deadline_kills_and_reaps_without_token_arguments(self):
        from unittest.mock import patch
        children, calls = [], []
        original = subprocess.Popen
        token = 'ghs_' + os.urandom(18).hex()
        def observed(*args, **kwargs):
            calls.append((args[0], kwargs['env']))
            child = original(*args, **kwargs); children.append(child); return child
        with patch.object(a.subprocess, 'Popen', observed):
            with self.assertRaisesRegex(a.AdmissionError, 'deadline'):
                a._bounded_get('/repos/' + a.REPOSITORY, token, timeout=.1,
                               worker_factory=lambda _: [sys.executable, '-c', 'import time;time.sleep(30)'])
        self.assertEqual(len(children), 1)
        self.assertIsNotNone(children[0].poll())
        self.assertLess(children[0].returncode, 0)
        self.assertTrue(children[0].stdout.closed and children[0].stderr.closed)
        self.assertTrue(all(token not in argument for argument in calls[0][0]), 'token escaped into child arguments')
        self.assertTrue(calls[0][1].get('GITHUB_TOKEN') == token, 'private credential transport mismatch')

    def test_actual_child_protocol_output_budgets_and_private_diagnostics(self):
        token = 'ghs_' + os.urandom(18).hex()
        valid = 'import sys,struct;sys.stdout.buffer.write(struct.pack(">I",0)+b\'{"ok":true}\')'
        raw, link = a._bounded_get('/repos/' + a.REPOSITORY, token,
                                  worker_factory=lambda _: [sys.executable, '-c', valid])
        self.assertEqual(a.parse(raw), {'ok': True}); self.assertEqual(link, '')
        programs = [
            'import os,sys,time;sys.stderr.write(os.environ["GITHUB_TOKEN"]);sys.stderr.flush();time.sleep(30)',
            'import os,sys;sys.stdout.write(os.environ["GITHUB_TOKEN"])',
            'import sys;sys.stdout.buffer.write(b"x"*' + str(a.BUNDLE_LIMIT + 16384 + 5) + ')',
        ]
        for index, program in enumerate(programs):
            with self.subTest(case=index), contextlib.redirect_stdout(io.StringIO()) as stdout, contextlib.redirect_stderr(io.StringIO()) as stderr:
                with self.assertRaises(a.AdmissionError) as error:
                    a._bounded_get('/repos/' + a.REPOSITORY, token,
                                   worker_factory=lambda _, body=program: [sys.executable, '-c', body])
                self.assertTrue(token not in str(error.exception) + stdout.getvalue() + stderr.getvalue(),
                                'worker diagnostic exposed a canary')

    def test_cancellation_reaps_only_the_owned_worker(self):
        from unittest.mock import patch
        children = []
        original = subprocess.Popen
        selector = a.selectors.DefaultSelector()
        def observed(*args, **kwargs):
            child = original(*args, **kwargs); children.append(child); return child
        with patch.object(a.subprocess, 'Popen', observed), patch.object(a.selectors, 'DefaultSelector', return_value=selector), patch.object(selector, 'select', side_effect=KeyboardInterrupt):
            with self.assertRaises(KeyboardInterrupt):
                a._bounded_get('/repos/' + a.REPOSITORY, 'synthetic-only',
                               worker_factory=lambda _: [sys.executable, '-c', 'import time;time.sleep(30)'])
        self.assertEqual(len(children), 1)
        self.assertIsNotNone(children[0].poll())
        self.assertLess(children[0].returncode, 0)
        self.assertTrue(children[0].stdout.closed and children[0].stderr.closed)


if __name__ == '__main__': unittest.main()
