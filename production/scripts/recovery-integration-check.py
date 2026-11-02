#!/usr/bin/env python3
"""Genuine isolated format-2 recovery, identity and operator-artifact acceptance."""
import argparse
import contextlib
import hashlib
import io
import json
from pathlib import Path
import secrets
import subprocess
import tarfile
import tempfile
import time
import types

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

import recovery_fixture as f

REQUIRED = ('R01_matching_restore_identity_roles_data_restart', 'R02_wrong_archive_key',
            'R03_tamper_truncation', 'R04_wrong_application_key_with_valid_manifest',
            'R05_missing_dependency_and_unsafe_members', 'R06_staged_active_config_mismatch',
            'R07_operator_tls_report_artifacts', 'R08_no_outbound_integration')


def must_reject(action, accepted=(ValueError, InvalidTag, FileNotFoundError, RuntimeError, AssertionError)):
    try:
        action()
    except accepted:
        return
    raise AssertionError('negative recovery procedure unexpectedly succeeded')


def envelope(args, path, values, *, manifest=True):
    values = dict(values)
    if manifest:
        meta = json.loads(values['manifest.json'])
        meta['dump_sha256'] = hashlib.sha256(values['database.dump']).hexdigest()
        meta['files'] = {n: {'size': len(v), 'sha256': hashlib.sha256(v).hexdigest()}
                         for n, v in values.items() if n != 'manifest.json'}
        values['manifest.json'] = json.dumps(meta).encode()
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode='w') as tar:
        for name, data in values.items():
            item = tarfile.TarInfo(name)
            item.size = len(data)
            item.mode = 0o600
            tar.addfile(item, io.BytesIO(data))
    nonce = secrets.token_bytes(12)
    f.private(path, b'GMABK01' + nonce + AESGCM(Path(args.key).read_bytes()).encrypt(
        nonce, buffer.getvalue(), b'GMA|Planner backup v1'))


def failed_restore_target(args, archive, mode):
    """Real owned DB/disk failure; not an invented failed subprocess result."""
    original = f.backup.run
    target = None
    reached = False
    disk_full = False
    failed = False

    def controlled(argv, data=None):
        nonlocal target, reached, disk_full
        argv = list(argv)
        if argv[0] == 'run' and '--name' in argv:
            target = argv[argv.index('--name') + 1]
            assert target.startswith('gma-restore-check-')
            if mode == 'disk_full':
                argv[argv.index('--tmpfs') + 1] = '/var/lib/postgresql:size=96m'
            argv[-1:-1] = ['--memory', '512m', '--cpus', '1', '--pids-limit', '128']
        if argv[0] == 'exec' and target in argv and 'pg_restore' in argv:
            result = subprocess.run(f.backup.DOCKER + argv, input=data, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, timeout=180)
            if result.returncode:
                if mode == 'disk_full':
                    logs = subprocess.run(f.backup.DOCKER + ['logs', '--tail', '100', target],
                                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)
                    disk_full = b'no space left on device' in (result.stderr + logs.stdout + logs.stderr).lower()
                raise RuntimeError('controlled restore target unavailable')
            return result.stdout
        result = original(argv, data)
        if argv[0] == 'exec' and target in argv and 'pg_isready' in argv and not reached:
            if mode == 'db_unavailable':
                original(['stop', '--time', '0', target])
            else:
                disk = original(['exec', target, 'df', '-Pk', '/var/lib/postgresql'])
                available = int(disk.decode().splitlines()[-1].split()[3])
                assert 64 < available <= 96 * 1024
                reached = True
                original(['exec', '--user', '0:0', target, 'dd', 'if=/dev/zero',
                          'of=/var/lib/postgresql/synthetic-restore-pressure', 'bs=1024', 'count=' + str(available - 64)])
                remaining = original(['exec', target, 'df', '-Pk', '/var/lib/postgresql'])
                assert int(remaining.decode().splitlines()[-1].split()[3]) <= 64
            reached = True
        return result

    f.backup.run = controlled
    try:
        with contextlib.redirect_stdout(io.StringIO()) as output:
            try:
                f.backup.verify(args, archive)
            except RuntimeError:
                failed = True
        assert reached and failed, 'real restore-target failure did not reach its intended oracle'
        assert 'runtime_verified' not in output.getvalue()
        if mode == 'disk_full':
            assert disk_full, 'disk-full negative lacked actual ENOSPC evidence'
    finally:
        f.backup.run = original
    return {'actual_failure': mode, 'enospc_observed': disk_full, 'runtime_success_emitted': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--candidate-source', type=Path, default=f.ROOT)
    f.image_arguments(parser)
    parser.add_argument('--evidence-file', type=Path)
    args = parser.parse_args()
    f.evidence(args.evidence_file)
    completed = []
    with tempfile.TemporaryDirectory(prefix='gm-vector-recovery-acceptance-') as temporary:
        root = Path(temporary)
        fixture = f.Fixture(root / 'installation', args.candidate_source, f.images(args))
        try:
            fixture.initialize()
            fixture.seed()
            columns, expected = fixture.fingerprint()
            original_schema = fixture.schema_state()
            backup_args, archive, artifacts, snapshot_bounds = fixture.snapshot(root / 'archives')
            values, meta, complete = f.backup.read_archive(backup_args, archive)
            assert complete and meta['format'] == 2
            assert not any(n.startswith(('reports/', 'tls/')) or 'private' in n for n in values)
            wrong_key = root / 'wrong-backup.key'
            f.private(wrong_key, secrets.token_bytes(32))
            wrong_args = types.SimpleNamespace(key=str(wrong_key))
            must_reject(lambda: f.backup.read_archive(wrong_args, archive), (InvalidTag,))
            f.receipt(REQUIRED[1]); completed.append(REQUIRED[1])
            raw = archive.read_bytes()
            for label, content in (('tamper', raw[:-1] + bytes([raw[-1] ^ 1])),
                                   ('truncated', raw[:-16]), ('bad-header', b'bad')):
                path = root / (label + '.gma')
                f.private(path, content)
                must_reject(lambda: f.backup.read_archive(backup_args, path))
            f.receipt(REQUIRED[2]); completed.append(REQUIRED[2])
            cfg = fixture.state / 'config'
            original_key = (cfg / 'encryption.key').read_bytes()
            f.private(cfg / 'encryption.key', secrets.token_bytes(32))
            must_reject(fixture.active_files, (ValueError,))
            f.private(cfg / 'encryption.key', original_key)
            fixture.active_files()
            f.receipt(REQUIRED[5]); completed.append(REQUIRED[5])
            for name, changed in (('missing-key', {k: v for k, v in values.items() if k != 'secrets/encryption.key'}),
                                  ('missing-smtp', {k: v for k, v in values.items() if k != 'secrets/smtp.json'}),
                                  ('unexpected-path', {**values, '../escape': b'synthetic'})):
                path = root / (name + '.gma')
                envelope(backup_args, path, changed)
                must_reject(lambda: f.backup.read_archive(backup_args, path))
            legacy = dict(values)
            legacy['manifest.json'] = json.dumps(dict(meta, format=1)).encode()
            legacy_path = root / 'legacy.gma'
            envelope(backup_args, legacy_path, legacy, manifest=False)
            assert f.backup.read_archive(backup_args, legacy_path)[2] is False
            with contextlib.redirect_stdout(io.StringIO()) as legacy_output:
                f.backup.verify(backup_args, legacy_path)
            legacy_receipt = json.loads(legacy_output.getvalue())
            assert legacy_receipt['legacy_database_only'] is True and legacy_receipt['runtime_verified'] is False
            frozen_config = fixture.active_files()
            archive_hash = hashlib.sha256(archive.read_bytes()).hexdigest()
            failures = [failed_restore_target(backup_args, archive, mode) for mode in ('db_unavailable', 'disk_full')]
            assert fixture.active_files() == frozen_config
            assert hashlib.sha256(archive.read_bytes()).hexdigest() == archive_hash
            fixture.admin.call('/auth/me')
            f.receipt(REQUIRED[4], target_failures=failures, source_and_archive_unchanged=True)
            completed.append(REQUIRED[4])
            wrong_application = dict(values)
            wrong_application['secrets/encryption.key'] = secrets.token_bytes(32)
            bad_application_path = root / 'wrong-application-key.gma'
            envelope(backup_args, bad_application_path, wrong_application)
            assert f.backup.read_archive(backup_args, bad_application_path)[2] is True
            with contextlib.redirect_stdout(io.StringIO()) as bad_output:
                must_reject(lambda: f.backup.verify(backup_args, bad_application_path), (ValueError,))
            assert 'runtime_verified' not in bad_output.getvalue()
            started = time.monotonic()
            fixture.restore(backup_args, archive, artifacts)
            assert fixture.schema_state() == original_schema, 'matching restoration did not recover the exact source schema'
            fixture.verify_usable(expected, columns)
            elapsed = time.monotonic() - started
            assert elapsed <= 1800, 'reference recovery exceeded accepted30-minute target'
            f.receipt(REQUIRED[0], seconds=round(elapsed, 3), snapshot_bounds=snapshot_bounds,
                      source=fixture.sha, images=fixture.manifest['images'],
                      last_recovered_ack='five_real_project_saves_before_snapshot')
            completed.append(REQUIRED[0])
            fixture.prove_ready_is_not_decryptability()
            f.receipt(REQUIRED[3], actual_ready_with_unusable_key_rejected=True)
            completed.append(REQUIRED[3])
            after = fixture.artifacts()
            for name in ('tls/fullchain.pem', 'tls/privkey.pem', 'nginx.conf', 'installation.json',
                         'reports/' + fixture.report_id + '.gma-bug'):
                assert after[name] == artifacts[name]
            certificate = fixture.state / 'tls/fullchain.pem'
            original = certificate.read_bytes()
            f.private(certificate, b'not-a-certificate')
            try:
                must_reject(lambda: fixture.installer.check_https(fixture.compose, fixture.env), (ValueError,))
            finally:
                f.private(certificate, original)
            fixture.installer.check_https(fixture.compose, fixture.env)
            f.receipt(REQUIRED[6]); completed.append(REQUIRED[6])
            fixture.assert_no_send()
            f.receipt(REQUIRED[7]); completed.append(REQUIRED[7])
            assert set(completed) == set(REQUIRED) and len(completed) == len(REQUIRED)
            print(json.dumps({'suite': 'matching_recovery', 'required': list(REQUIRED), 'passed': completed,
                              'status': 'PASS', 'reference_only': True}), flush=True)
        finally:
            fixture.cleanup()


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        f.failure('matching_recovery', error)
        raise SystemExit(1)
