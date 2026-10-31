"""Inert generated reports/archives test admission, never a vulnerable image."""
import copy
import datetime as dt
import gzip
import io
import json
import os
import subprocess
import sys
from pathlib import Path
import tarfile
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import image_gate as gate
from image_producer import docker_command, write_json, produce, main, run_bounded


NOW = dt.datetime(2026, 10, 31, 12, tzinfo=dt.timezone.utc)


def tar_bytes(files):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w') as archive:
        for name, contents in files:
            info = tarfile.TarInfo(name)
            info.size = len(contents)
            archive.addfile(info, io.BytesIO(contents))
    return output.getvalue()


def archive_fixture(path, compressed=False):
    # Two layers, with the first marker removed by a later whiteout. Privacy
    # fixtures reuse this independent representation, not a running container.
    layers = [tar_bytes([('app/first.txt', b'first')]),
              tar_bytes([('app/.wh.first.txt', b''), ('app/planner', b'inert test binary')])]
    diffs = ['sha256:' + gate.sha256_bytes(value) for value in layers]
    config = json.dumps({'rootfs': {'type': 'layers', 'diff_ids': diffs}}).encode()
    image_id = 'sha256:' + gate.sha256_bytes(config)
    names = ['blobs/sha256/' + gate.sha256_bytes(value) for value in layers]
    config_name = 'blobs/sha256/' + gate.sha256_bytes(config)
    manifest = [{'Config': config_name, 'RepoTags': [], 'Layers': names}]
    files = [('manifest.json', json.dumps(manifest).encode()), (config_name, config)]
    files += list(zip(names, [gzip.compress(x, mtime=0) if compressed else x for x in layers]))
    path.write_bytes(tar_bytes(files))
    return image_id, diffs


def report_fixture(label, image):
    family = 'debian' if label == 'app' else 'alpine'
    results = [{'Target': family, 'Class': 'os-pkgs', 'Type': family,
                'Packages': [{'Name': 'os-library', 'Version': '1',
                              'Identifier': {'PURL': 'pkg:generic/os-library@1'}}]}]
    if label == 'app':
        results.append({'Target': 'app/planner', 'Class': 'lang-pkgs', 'Type': 'gobinary',
                        'Packages': [{'Name': 'stdlib', 'Version': '1',
                                      'Identifier': {'PURL': 'pkg:golang/stdlib@1'}}]})
    report = {'SchemaVersion': 2, 'Trivy': {'Version': '0.76.1'},
              'ArtifactType': 'container_image', 'CreatedAt': '2026-10-31T11:59:00Z',
              'Metadata': {'ImageID': image['image_id'], 'DiffIDs': image['diff_ids'],
                           'OS': {'Family': family}}, 'Results': results}
    sbom = {'bomFormat': 'CycloneDX', 'specVersion': '1.7',
            'metadata': {'timestamp': '2026-10-31T11:59:30Z',
                         'tools': {'components': [{'name': 'trivy', 'version': '0.76.1'}]},
                         'component': {'type': 'container', 'properties': [
                             {'name': 'aquasecurity:trivy:ImageID', 'value': image['image_id']},
                             *[{'name': 'aquasecurity:trivy:DiffID', 'value': x}
                               for x in reversed(image['diff_ids'])]]}},
            'components': [{'name': p['Name'], 'purl': p['Identifier']['PURL']}
                           for result in results for p in result['Packages']]}
    return report, sbom


class ImageGateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'cache' / 'db').mkdir(parents=True)
        (self.root / 'cache' / 'db' / 'trivy.db').write_bytes(b'inert advisory fixture')
        metadata = {'Version': 2, 'UpdatedAt': '2026-10-31T06:00:00Z',
                    'DownloadedAt': '2026-10-31T11:00:00Z'}
        write_json(self.root / 'db-metadata.json', metadata)
        self.manifest = {'format': 'gm-vector-image-scan-v1', 'source_revision': 'b' * 40,
                         'candidate_release': {'git_commit': 'b' * 40},
                         'scanner': {'image': gate.SCANNER, 'version': '0.76.1'},
                         'approval_mode': 'disabled', 'exceptions': [],
                         'started_at': '2026-10-31T11:00:00Z', 'finished_at': NOW.isoformat(),
                         'db_updated_at': metadata['UpdatedAt'],
                         'db_metadata_sha256': gate.file_hash(self.root / 'db-metadata.json'),
                         'db_sha256': gate.file_hash(self.root / 'cache' / 'db' / 'trivy.db'), 'images': {}}
        for label in gate.LABELS:
            archive = self.root / (label + '-image.tar')
            image_id, diffs = archive_fixture(archive, compressed=(label == 'proxy'))
            image = {'image_id': image_id, 'diff_ids': diffs,
                     'conversion_started_at': '2026-10-31T11:59:30.100000Z',
                     'conversion_finished_at': '2026-10-31T11:59:30.900000Z',
                     'archive_inventory': gate.archive_inventory(archive, image_id, diffs),
                     'archive_sha256': gate.file_hash(archive)}
            report, sbom = report_fixture(label, image)
            write_json(self.root / (label + '-vulns.json'), report)
            write_json(self.root / (label + '-sbom.json'), sbom)
            for suffix, key in [('vulns.json', 'report_sha256'), ('sbom.json', 'sbom_sha256')]:
                image[key] = gate.file_hash(self.root / (label + '-' + suffix))
            self.manifest['images'][label] = image
        write_json(self.root / 'manifest.json', self.manifest)

    def rewrite(self, name, value):
        (self.root / name).write_text(json.dumps(value))

    def change_report(self, change, label='app', sbom_change=None):
        report = gate.read_json(self.root / (label + '-vulns.json'))
        change(report)
        self.rewrite(label + '-vulns.json', report)
        self.manifest['images'][label]['report_sha256'] = gate.file_hash(self.root / (label + '-vulns.json'))
        if sbom_change:
            sbom = gate.read_json(self.root / (label + '-sbom.json'))
            sbom_change(sbom)
            self.rewrite(label + '-sbom.json', sbom)
            self.manifest['images'][label]['sbom_sha256'] = gate.file_hash(self.root / (label + '-sbom.json'))
        self.rewrite('manifest.json', self.manifest)

    def validate(self):
        return gate.validate_directory(self.root, NOW)

    def test_complete_clean_inventory_and_reordered_cdx_layers_pass(self):
        self.assertEqual(self.validate()['status'], 'PASS')
        self.assertEqual(set(self.validate()['images']), {'app', 'proxy', 'postgres'})

    def test_unfixed_high_and_unknown_are_blocked(self):
        for severity in ('HIGH', 'CRITICAL', 'UNKNOWN'):
            with self.subTest(severity=severity):
                def change(report):
                    report['Results'][0]['Vulnerabilities'] = [{'Severity': severity,
                        'VulnerabilityID': 'TEST-ADVISORY-1', 'PkgName': 'os-library', 'InstalledVersion': '1'}]
                self.change_report(change)
                result = self.validate()
                self.assertEqual(result['status'], 'BLOCKED')
                self.assertEqual(result['images']['app']['blocking'][0]['fixed_version'], '')

    def test_low_medium_are_retained_without_becoming_blocking(self):
        self.change_report(lambda r: r['Results'][0].update(Vulnerabilities=[
            {'Severity': severity, 'VulnerabilityID': 'TEST-' + severity, 'PkgName': 'os-library',
             'InstalledVersion': '1'} for severity in ('LOW', 'MEDIUM')]))
        self.assertEqual(len(self.validate()['images']['app']['findings']), 2)
        self.assertEqual(self.validate()['status'], 'PASS')

    def test_language_scan_omitted_from_both_outputs_is_rejected(self):
        self.change_report(lambda r: r['Results'].pop(), sbom_change=lambda s: s['components'].pop())
        with self.assertRaisesRegex(gate.GateError, 'Go package scan omitted'):
            self.validate()

    def test_missing_os_result_is_rejected(self):
        self.change_report(lambda r: r['Results'].pop(0))
        with self.assertRaisesRegex(gate.GateError, 'OS package scan omitted'):
            self.validate()

    def test_invalid_or_missing_severity_is_not_clean(self):
        for severity in ('high', 'UNRATED', None):
            self.change_report(lambda r: r['Results'][0].update(Vulnerabilities=[{
                'VulnerabilityID': 'TEST', 'PkgName': 'x', 'InstalledVersion': '1', 'Severity': severity}]))
            with self.assertRaises(gate.GateError):
                self.validate()

    def test_fresh_download_does_not_fix_stale_or_future_updated_at(self):
        for value in ('2026-10-30T11:59:59Z', '2026-11-01T00:00:00Z', '2026-10-31T01:00:00', 'invalid'):
            with self.subTest(value=value):
                metadata = {'Version': 2, 'UpdatedAt': value, 'DownloadedAt': NOW.isoformat()}
                with self.assertRaises(gate.GateError):
                    gate.validate_db(metadata, NOW)

    def test_exact_24_hour_boundary_is_accepted(self):
        gate.validate_db({'Version': 2, 'UpdatedAt': '2026-10-30T12:00:00Z'}, NOW)

    def test_image_omission_or_waiver_or_stale_binary_blocks(self):
        for change in (lambda m: m['images'].pop('postgres'),
                       lambda m: m.update(exceptions=[{'approved_by': 'owner'}]),
                       lambda m: m['candidate_release'].update(git_commit='c' * 40)):
            value = copy.deepcopy(self.manifest)
            change(value)
            self.rewrite('manifest.json', value)
            with self.assertRaises(gate.GateError):
                self.validate()

    def test_swapped_db_bytes_are_rejected(self):
        (self.root / 'cache' / 'db' / 'trivy.db').write_bytes(b'replaced database')
        with self.assertRaisesRegex(gate.GateError, 'database content mismatch'):
            self.validate()

    def test_wrong_report_image_layer_or_scan_window_is_rejected(self):
        for change in (lambda r: r['Metadata'].update(ImageID='sha256:' + '0' * 64),
                       lambda r: r['Metadata'].update(DiffIDs=[]),
                       lambda r: r.update(CreatedAt='2026-10-30T12:00:00Z')):
            report, _ = report_fixture('app', self.manifest['images']['app'])
            change(report)
            self.change_report(lambda r: r.update(report))
            with self.assertRaises(gate.GateError):
                self.validate()

    def test_mutated_or_omitted_artifact_cannot_reuse_manifest(self):
        for suffix in ('image.tar', 'vulns.json', 'sbom.json'):
            original = (self.root / ('app-' + suffix)).read_bytes()
            (self.root / ('app-' + suffix)).write_bytes(b'truncated')
            with self.assertRaises(gate.GateError):
                self.validate()
            (self.root / ('app-' + suffix)).write_bytes(original)

    def test_duplicate_json_keys_and_empty_and_oversized_inputs_fail(self):
        for raw in (b'{"Results":[],"Results":[1]}', b'', b'NaN', b'[' * 1100):
            with self.assertRaises((gate.GateError, RecursionError)):
                value = gate.json_bytes(raw)
                if not isinstance(value, dict):
                    raise gate.GateError('invalid object')
        with self.assertRaises(gate.GateError):
            gate.json_bytes(b'x' * (gate.JSON_LIMIT + 1))

    def test_unsafe_archive_names_or_bad_layer_hash_fail(self):
        for name in ('../outside', '/absolute', 'a/../../b', 'a\\b'):
            with self.assertRaises(gate.GateError):
                gate.safe_member(name)
        image = self.manifest['images']['app']
        with self.assertRaises(gate.GateError):
            gate.archive_inventory(self.root / 'app-image.tar', image['image_id'], ['sha256:' + '0' * 64] * 2)

    def test_private_files_are_exclusive_and_remote_docker_refused(self):
        path = self.root / 'private.json'
        write_json(path, {'synthetic': True})
        self.assertEqual(os.stat(path).st_mode & 0o777, 0o600)
        with self.assertRaises(FileExistsError):
            write_json(path, {})
        with self.assertRaises(gate.GateError):
            docker_command('tcp://example.test:2375')

    def test_unsupported_group_or_unrelated_go_target_or_old_epoch_fails(self):
        for change in (
            lambda r: r['Results'][1].update(Type='unsupported'),
            lambda r: r['Results'][1].update(Target='opt/unrelated-helper'),
            lambda r: r.update(CreatedAt='2026-10-31T05:00:00Z'),
        ):
            report, _ = report_fixture('app', self.manifest['images']['app'])
            change(report)
            self.change_report(lambda r: r.update(report))
            with self.assertRaises(gate.GateError):
                self.validate()

    def test_wrong_sbom_root_or_conversion_window_fails(self):
        for change in (
            lambda s: s['metadata']['component'].update(type='device'),
            lambda s: s['metadata'].update(timestamp='2026-10-31T11:58:00Z'),
        ):
            _, sbom = report_fixture('app', self.manifest['images']['app'])
            change(sbom)
            self.change_report(lambda r: None, sbom_change=lambda s: s.update(sbom))
            with self.assertRaises(gate.GateError):
                self.validate()

    def test_output_parent_symlink_or_dotdot_cannot_enter_source(self):
        fake_source = self.root / 'repo'
        fake_source.mkdir()
        (self.root / 'link').symlink_to(fake_source, target_is_directory=True)
        for output in (self.root / 'link' / 'raw', self.root / 'unused' / '..' / 'repo' / 'raw'):
            args = SimpleNamespace(source_revision='b' * 40, output_dir=output)
            with patch('image_producer.__file__', str(fake_source / 'production/scripts/image_producer.py')):
                with self.assertRaisesRegex(gate.GateError, 'outside the repository'):
                    produce(args)
            self.assertFalse((fake_source / 'raw').exists())

    def test_malformed_archive_and_timestamp_never_print_input_or_traceback(self):
        for failure in (tarfile.ReadError('PRIVATE_MARKER'), EOFError('PRIVATE_MARKER'),
                        OverflowError('PRIVATE_MARKER')):
            args = ['scan-images.py', '--app-image', 'a', '--proxy-image', 'b', '--postgres-image', 'c',
                    '--source-revision', 'b' * 40, '--output-dir', str(self.root / 'new')]
            with patch('sys.argv', args), patch('image_producer.produce', side_effect=failure), \
                 patch('sys.stderr', new_callable=io.StringIO) as output:
                self.assertEqual(main(), 2)
                self.assertNotIn('PRIVATE_MARKER', output.getvalue())
                self.assertNotIn('Traceback', output.getvalue())
        with self.assertRaises(gate.GateError):
            gate.timestamp('0001-01-01T00:00:00+23:00')

    def test_noisy_child_output_is_bounded_while_it_is_written(self):
        log = self.root / 'bounded.log'
        with self.assertRaises(gate.GateError):
            run_bounded([sys.executable, '-c', 'import os; os.write(1,b"X"*(2*1024*1024)); os.write(1,b"Y")'],
                        log, self.root, os.environ.copy(), capture=True)
        self.assertLessEqual(log.stat().st_size, 1024 * 1024)
        self.assertLessEqual(log.with_suffix('.stdout').stat().st_size, 1024 * 1024)
        self.assertEqual(log.with_suffix('.stdout').stat().st_mode & 0o777, 0o600)

    def test_archive_channel_is_not_truncated_by_metadata_limit(self):
        archive = self.root / 'larger-artifact.tar'
        run_bounded([sys.executable, '-c', 'import os; os.write(1,b"X"*(2*1024*1024))'],
                    self.root / 'archive.log', self.root, os.environ.copy(), artifact_output=archive)
        self.assertEqual(archive.stat().st_size, 2 * 1024 * 1024)
        self.assertEqual(archive.stat().st_mode & 0o777, 0o600)

    def test_child_deadline_is_not_an_empty_success(self):
        with self.assertRaises(subprocess.TimeoutExpired):
            run_bounded([sys.executable, '-c', 'import time; time.sleep(10)'],
                        self.root / 'timeout.log', self.root, os.environ.copy(), timeout=0.03)

    def test_malformed_cli_does_not_echo_argument_values(self):
        args = ['scan-images.py', '--app-image', 'a', '--proxy-image', 'b', '--postgres-image', 'c',
                '--source-revision', 'b' * 40, '--output-dir', str(self.root / 'new'),
                '--unexpected=PRIVATE_INERT_MARKER']
        with patch('sys.argv', args), patch('sys.stderr', new_callable=io.StringIO) as output:
            with self.assertRaises(SystemExit) as result:
                main()
            self.assertEqual(result.exception.code, 2)
            self.assertNotIn('PRIVATE_INERT_MARKER', output.getvalue())
