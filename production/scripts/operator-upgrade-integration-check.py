#!/usr/bin/env python3
"""Fresh accepted-origin to candidate migration and matching rollback fixtures."""
import argparse
import contextlib
import io
import json
import os
from pathlib import Path
import secrets
import tempfile
import time

import recovery_fixture as f

REQUIRED = ('U01_original_identity_and_rich_seed', 'U02_six_file_admission',
            'U03_successful_forward_migration', 'U04_failed_candidate_matched_rollback',
            'U05_restored_identity_data_privileges_restart', 'U06_rto_rpo_and_operator_artifacts')


def guard(source, candidate):
    updater = f.module(candidate / 'production/deploy/update_engine.py', 'fixture_update_' + secrets.token_hex(6))
    matches = all((source / name).is_file() and (candidate / name).is_file() and
                  (source / name).read_bytes() == (candidate / name).read_bytes() for name in f.CONTRACT)
    try:
        updater.Engine.compatible(None, source, candidate)
    except ValueError as error:
        assert not matches and str(error) == 'infrastructure_change_requires_operator'
    else:
        assert matches
    return updater, matches


def candidate_context(fixture, source, installer, manifest, sha):
    fixture.source = source
    fixture.installer = installer
    fixture.manifest = manifest
    fixture.sha = sha
    fixture.compose, fixture.env = installer.context(fixture.state, manifest)


def mutate_failed_candidate(fixture):
    fixture.sql("CREATE TABLE synthetic_failed_upgrade_marker(value text); INSERT INTO synthetic_failed_upgrade_marker VALUES('controlled'); UPDATE projects SET title='Synthetic failed migration' WHERE id='recovery-project-0';")


def operator_activate(fixture, candidate, manifest, sha, *, fail=False):
    fixture.stop_writers()
    installer = f.module(candidate / 'production/deploy/install.py', 'fixture_candidate_install_' + secrets.token_hex(6))
    candidate_context(fixture, candidate, installer, manifest, sha)
    original = installer.run
    injected = False

    def observed(argv, *args, **kwargs):
        nonlocal injected
        result = original(argv, *args, **kwargs)
        if fail and not injected and argv[-1:] == ['migrate'] and 'run' in argv:
            injected = True
            mutate_failed_candidate(fixture)
            raise RuntimeError('controlled_candidate_activation_failure')
        return result

    installer.run = observed
    try:
        with contextlib.redirect_stdout(io.StringIO()):
            installer.prepare(fixture.state, manifest)
    finally:
        installer.run = original
    assert not fail and not injected
    fixture.stop_worker()
    fixture.admin = fixture.client()
    fixture.assert_release()


def prepared_engine(fixture, updater, candidate, manifest, sha):
    """Exercise real apply/recover with independently pinned pre-staged inputs.

    Git transport/build/check is a separate existing updater acceptance gate.
    No network fetch/build is silently substituted into this recovery timer.
    """
    updater.Engine.compatible(None, fixture.source, candidate)
    fixture.installer.run(fixture.compose + ['up', '-d', '--no-deps', 'worker'], fixture.env, capture=True)
    engine = updater.Engine(fixture.state, candidate)
    previous = engine.manifest()
    engine.record = {'state': 'ready', 'id': secrets.token_hex(16), 'commit': sha,
                     'source': str(candidate), 'previous_source': str(fixture.source),
                     'previous': previous, 'images': manifest['images']}
    engine.save()
    return engine


def engine_failure_wrapper(updater, engine, fixture):
    original_module = updater.module
    injected = []

    def observed(path, name):
        value = original_module(path, name)
        if Path(path).name == 'install.py':
            original_run = value.run

            def run(argv, *args, **kwargs):
                result = original_run(argv, *args, **kwargs)
                if engine.record.get('migration_started') and not injected and argv[-1:] == ['migrate'] and 'run' in argv:
                    injected.append(True)
                    mutate_failed_candidate(fixture)
                    raise RuntimeError('controlled_candidate_activation_failure')
                return result

            value.run = run
        return value

    updater.module = observed
    return original_module, injected


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--origin-source', type=Path, required=True)
    parser.add_argument('--candidate-source', type=Path, default=f.ROOT)
    f.image_arguments(parser, 'origin-')
    f.image_arguments(parser, 'candidate-')
    parser.add_argument('--evidence-file', type=Path)
    args = parser.parse_args()
    f.evidence(args.evidence_file)
    source, candidate = args.origin_source.resolve(), args.candidate_source.resolve()
    origin_sha, candidate_sha = f.source_identity(source), f.source_identity(candidate)
    assert origin_sha in f.ORIGINS and origin_sha != candidate_sha
    f.command(['git', '-C', str(candidate), 'merge-base', '--is-ancestor', origin_sha, candidate_sha])
    updater, matches = guard(source, candidate)
    candidate_installer = f.module(candidate / 'production/deploy/install.py', 'candidate_image_resolver')
    candidate_manifest = f.image_manifest(candidate_installer, f.images(args, 'candidate-'))
    completed = []
    timings = []
    admission = None
    with tempfile.TemporaryDirectory(prefix='gm-vector-origin-acceptance-') as temp:
        for branch in ('forward', 'rollback'):
            fixture = f.Fixture(Path(temp) / branch, source, f.images(args, 'origin-'), source_sha=origin_sha)
            engine = None
            try:
                fixture.initialize()
                fixture.seed()
                columns, expected = fixture.fingerprint()
                original_schema = fixture.schema_state()
                backup_args, archive, artifacts, bounds = fixture.snapshot(fixture.root / 'archives')
                origin_installer, origin_manifest = fixture.installer, fixture.manifest
                same_infrastructure_images = all(origin_manifest['images'][k] == candidate_manifest['images'][k]
                                                 for k in ('db', 'proxy'))
                app_only = origin_sha == f.ORIGINS[2] and matches and same_infrastructure_images
                current_admission = 'app_only_pre_staged_inputs' if app_only else 'operator_assisted'
                if admission is None:
                    admission = current_admission
                    f.receipt(REQUIRED[0], origin=origin_sha, projects=5, tasks=1500, staff=30, retained_facts=1001)
                    completed.append(REQUIRED[0])
                    f.receipt(REQUIRED[1], contract_match=matches, infrastructure_images_match=same_infrastructure_images,
                              admission=admission, guard_preserved=True)
                    completed.append(REQUIRED[1])
                else:
                    assert admission == current_admission
                if branch == 'forward':
                    migration_proof = fixture.prove_migrations(candidate, candidate_manifest, original_schema, expected, columns)
                    if app_only:
                        # This origin already has the target schema; restoring its
                        # unchanged services permits the real Engine.assert_current.
                        with contextlib.redirect_stdout(io.StringIO()):
                            fixture.installer.prepare(fixture.state, fixture.manifest)
                        engine = prepared_engine(fixture, updater, candidate, candidate_manifest, candidate_sha)
                        engine.apply()
                        assert engine.status()['state'] == 'succeeded'
                        candidate_context(fixture, candidate, candidate_installer, candidate_manifest, candidate_sha)
                        fixture.stop_worker()
                        fixture.admin = fixture.client()
                    else:
                        operator_activate(fixture, candidate, candidate_manifest, candidate_sha)
                    fixture.verify_usable(expected, columns)
                    target_versions = fixture.schema_state()['versions']
                    assert target_versions == fixture.candidate_schema_versions()
                    assert target_versions[:len(original_schema['versions'])] == original_schema['versions']
                    fixture.verify_new_features()
                    f.receipt(REQUIRED[2], origin=origin_sha, candidate=candidate_sha, admission=admission,
                              actual_images=candidate_manifest['images'], migration_proof=migration_proof)
                    completed.append(REQUIRED[2])
                else:
                    if app_only:
                        engine = prepared_engine(fixture, updater, candidate, candidate_manifest, candidate_sha)
                        original_module, injected = engine_failure_wrapper(updater, engine, fixture)
                        try:
                            engine.execute('apply')
                        finally:
                            updater.module = original_module
                        assert injected and engine.status()['state'] == 'recovery_required'
                    else:
                        try:
                            operator_activate(fixture, candidate, candidate_manifest, candidate_sha, fail=True)
                        except RuntimeError as error:
                            assert str(error) == 'controlled_candidate_activation_failure'
                        else:
                            raise AssertionError('controlled candidate failure was not reached')
                    assert fixture.sql("SELECT to_regclass('public.synthetic_failed_upgrade_marker') IS NOT NULL") == 't'
                    assert fixture.sql("SELECT title FROM projects WHERE id='recovery-project-0'") == 'Synthetic failed migration'
                    assert not fixture.installer.run(fixture.compose + ['ps', '--status', 'running', '-q', 'api', 'worker', 'proxy'],
                                                     fixture.env, capture=True)
                    started = time.monotonic()
                    if app_only:
                        engine.recover()
                        assert engine.status()['state'] == 'succeeded'
                        candidate_context(fixture, source, origin_installer, origin_manifest, origin_sha)
                        fixture.stop_worker()
                        fixture.admin = fixture.client()
                    else:
                        fixture.restore(backup_args, archive, artifacts, installer=origin_installer,
                                        manifest=origin_manifest, sha=origin_sha)
                    assert fixture.sql("SELECT to_regclass('public.synthetic_failed_upgrade_marker') IS NULL") == 't'
                    assert fixture.schema_state() == original_schema, 'rollback left a candidate schema behind the origin image'
                    fixture.verify_usable(expected, columns)
                    elapsed = time.monotonic() - started
                    assert elapsed <= 1800
                    timings.append({'seconds': round(elapsed, 3), 'snapshot_bounds': bounds,
                                    'later_lost_acknowledged_writes': [],
                                    'last_recovered_ack': 'five_real_project_saves_before_snapshot'})
                    f.receipt(REQUIRED[3], origin=origin_sha, real_candidate_migration=True, matching_snapshot=True)
                    completed.append(REQUIRED[3])
                    f.receipt(REQUIRED[4], origin=origin_sha, actual_images=origin_manifest['images'])
                    completed.append(REQUIRED[4])
                    restored = fixture.artifacts()
                    for name, value in artifacts.items():
                        if name != 'deployment.json':
                            assert restored[name] == value
                    f.receipt(REQUIRED[5], origin=origin_sha, reference_only=True, timings=timings)
                    completed.append(REQUIRED[5])
            finally:
                if engine is not None:
                    os.close(engine.lock)
                fixture.cleanup()
    assert completed == list(REQUIRED)
    print(json.dumps({'suite': 'origin_upgrade_recovery', 'origin': origin_sha, 'candidate': candidate_sha,
                      'admission': admission, 'required': list(REQUIRED), 'passed': completed, 'status': 'PASS'}), flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        f.failure('origin_upgrade_recovery', error)
        raise SystemExit(1)
