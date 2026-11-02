#!/usr/bin/env python3
"""Real, disposable online backup snapshot regression; never reads host config."""
import argparse
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import secrets
import tempfile
import time
import types
import subprocess

spec = importlib.util.spec_from_file_location('coherence_backup', Path(__file__).with_name('backup.py'))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)
REQUIRED = ('C01_online_dump_manifest_snapshot', 'C02_snapshot_owner_failure_cleanup',
            'C03_snapshot_timeout_cleanup')


def private_write(path, data):
    path.write_bytes(data)
    path.chmod(0o600)


def online_snapshot(root):
    name = 'gm-vector-coherence-' + secrets.token_hex(6)
    original = b.run
    fired = False
    try:
        original(['run', '-d', '--name', name, '--network', 'none', '--memory', '512m', '--cpus', '1',
                  '--pids-limit', '128', '--tmpfs', '/var/lib/postgresql:size=384m',
                  '-e', 'POSTGRES_HOST_AUTH_METHOD=trust', '-e', 'POSTGRES_USER=planner',
                  '-e', 'POSTGRES_DB=planner', b.DB_IMAGE])
        for _ in range(40):
            try:
                original(['exec', name, 'psql', '-U', 'planner', '-d', 'planner', '-Atc', 'SELECT 1'])
                break
            except RuntimeError:
                time.sleep(.25)
        else:
            raise AssertionError('synthetic database did not become ready')
        sql = ';'.join('CREATE TABLE ' + table + ' (id text PRIMARY KEY)' for table in
                       ('projects', 'tasks', 'dependencies', 'users', 'time_entries', 'calendars'))
        sql += ";INSERT INTO users VALUES('before-snapshot');"
        original(['exec', name, 'psql', '-U', 'planner', '-d', 'planner', '-v', 'ON_ERROR_STOP=1', '-c', sql])
        expected = b.counts(name)
        cfg = root / 'config'
        cfg.mkdir(mode=0o700)
        for filename in b.CORE:
            private_write(cfg / filename, b'S' * 32 if filename == 'encryption.key' else b'synthetic')
        private_write(cfg / 'config.json', json.dumps({'secret_file': '/run/secrets/encryption.key'}).encode())
        args = types.SimpleNamespace(config=str(cfg), destination=str(root / 'archives'),
                                     key=str(root / 'backup.key'), container=name)

        def controlled_run(argv, data=None):
            nonlocal fired
            result = original(argv, data)
            if argv[0] == 'exec' and 'pg_dump' in argv and not fired:
                original(['exec', name, 'psql', '-U', 'planner', '-d', 'planner', '-v', 'ON_ERROR_STOP=1',
                          '-c', "INSERT INTO users VALUES('committed-after-real-dump')"])
                fired = True
            return result

        b.run = controlled_run
        with contextlib.redirect_stdout(io.StringIO()):
            archive = b.backup(args)
        b.run = original
        assert fired, 'real dump completion barrier was not reached'
        values, meta, complete = b.read_archive(args, archive)
        assert complete
        assert b.counts(name) != expected, 'controlled writer did not commit before backup returned'
        original(['exec', name, 'createdb', '-U', 'planner', 'coherence_restore'])
        original(['exec', '-i', name, 'pg_restore', '-U', 'planner', '-d', 'coherence_restore',
                  '--no-owner', '--no-acl', '--exit-on-error'], values['database.dump'])
        restored = b.counts(name, 'coherence_restore')
        assert restored == expected, 'dump did not retain its pre-write snapshot'
        assert restored == meta['counts'], 'C01 manifest counted a different snapshot than the actual restored dump'
        print(json.dumps({'scenario': 'C01_online_dump_manifest_snapshot', 'status': 'PASS',
                          'actual_dump_restore': True, 'concurrent_writer_committed': True}), flush=True)
        failure_cases(root, name, args, original)
    finally:
        b.run = original
        original(['rm', '-f', name])


def failure_cases(root, name, args, original):
    def owner_count():
        return original(['exec', name, 'psql', '-X', '-qAt', '-U', 'planner', '-d', 'planner', '-c',
                         "SELECT count(*) FROM pg_stat_activity WHERE application_name LIKE 'gm-vector-backup-%'"]).decode().strip()
    for scenario in REQUIRED[1:]:
        reached = False
        args.destination = str(root / scenario)

        def controlled_run(argv, data=None):
            nonlocal reached
            if argv[0] == 'exec' and 'pg_dump' in argv:
                assert owner_count() == '1', 'actual exported snapshot owner missing'
                reached = True
                if scenario == REQUIRED[1]:
                    terminated = original(['exec', name, 'psql', '-X', '-qAt', '-U', 'planner', '-d', 'planner', '-c',
                        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name LIKE 'gm-vector-backup-%'"])
                    assert terminated.decode().strip() == 't'
                else:
                    raise subprocess.TimeoutExpired(['synthetic-pg-dump'], 180)
            return original(argv, data)

        b.run = controlled_run
        try:
            with contextlib.redirect_stdout(io.StringIO()):
                b.backup(args)
        except (RuntimeError, subprocess.TimeoutExpired):
            pass
        else:
            raise AssertionError('failed snapshot capture unexpectedly published an archive')
        finally:
            b.run = original
        assert reached, 'controlled failure did not reach a real exported snapshot'
        assert owner_count() == '0', 'exported snapshot transaction leaked'
        assert not list(Path(args.destination).glob('*.gma')), 'partial archive was published'
        # A normal independent writer still succeeds after exporter cleanup.
        original(['exec', name, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-U', 'planner', '-d', 'planner',
                  '-c', "INSERT INTO users VALUES('after-" + scenario + "')"])
        if scenario == REQUIRED[2]:
            blocked_dump_cleanup(name, args, original)
        print(json.dumps({'scenario': scenario, 'status': 'PASS', 'leaked_snapshot_owners': 0,
                          'published_archives': 0, 'failure': 'actual_owner_termination' if scenario == REQUIRED[1]
                          else 'injected_timeout_and_actual_blocked_pg_dump_timeout'}), flush=True)


def blocked_dump_cleanup(name, args, original):
    """A real lock wait proves whether killing docker exec also kills pg_dump."""
    locker = None
    dump = None
    reached = False
    application = 'coherence-blocked-dump'
    args.destination += '-actual-blocked-dump'

    def query(sql):
        return original(['exec', name, 'psql', '-X', '-qAt', '-U', 'planner', '-d', 'planner', '-c', sql]).decode().strip()

    def barrier(sql):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if query(sql) == '1':
                return
        raise AssertionError('actual PostgreSQL lock barrier was not reached')

    def timed_run(argv, data=None):
        nonlocal locker, dump, reached, application
        if argv[0] != 'exec' or 'pg_dump' not in argv:
            return original(argv, data)
        locker = subprocess.Popen(b.DOCKER + ['exec', '-i', name, 'psql', '-X', '-qAt', '-v',
                    'ON_ERROR_STOP=1', '-U', 'planner', '-d', 'planner'], stdin=subprocess.PIPE,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        locker.stdin.write(b"BEGIN; SET LOCAL application_name='coherence-exclusive-lock'; LOCK TABLE users IN ACCESS EXCLUSIVE MODE;\n")
        locker.stdin.flush()
        # The snapshot's AccessShare lock makes this real exclusive lock queue.
        barrier("SELECT count(*) FROM pg_stat_activity WHERE application_name='coherence-exclusive-lock' AND wait_event_type='Lock'")
        environment = next((value for value in argv if value.startswith('PGAPPNAME=')), None)
        if environment:
            application = environment.split('=', 1)[1]
            actual_argv = argv
        else:
            actual_argv = [argv[0], '-e', 'PGAPPNAME=' + application] + argv[1:]
        dump = subprocess.Popen(b.DOCKER + actual_argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        barrier("SELECT count(*) FROM pg_stat_activity WHERE application_name='" + application + "' AND wait_event_type='Lock'")
        reached = True
        try:
            dump.communicate(timeout=.05)  # Test-only caller budget after a DB wait, not a sleep.
        except subprocess.TimeoutExpired:
            dump.kill()
            dump.communicate(timeout=2)
            raise
        raise AssertionError('actual blocked pg_dump unexpectedly completed')

    b.run = timed_run
    try:
        try:
            with contextlib.redirect_stdout(io.StringIO()):
                b.backup(args)
        except subprocess.TimeoutExpired:
            pass
        else:
            raise AssertionError('real blocked dump timeout was not propagated')
        assert reached
        assert not list(Path(args.destination).glob('*.gma'))
        leaked = query("SELECT count(*) FROM pg_stat_activity WHERE application_name='" + application + "'")
        assert leaked == '0', 'C03 actual timed-out docker exec left a remote pg_dump DB session alive'
    finally:
        b.run = original
        # Clean only this controlled fixture, including the deliberately leaked
        # baseline session; the RED itself must not leave resources behind.
        query("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name IN ('" + application + "','coherence-exclusive-lock')")
        if locker:
            try:
                locker.stdin.close()
                locker.wait(timeout=2)
            except (BrokenPipeError, subprocess.TimeoutExpired):
                locker.kill();locker.wait(timeout=2)
        if dump:
            if dump.poll() is None:
                dump.kill();dump.wait(timeout=2)
            dump.stdout.close();dump.stderr.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence-file', type=Path)
    args = parser.parse_args()
    if args.evidence_file:
        fd = os.open(args.evidence_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        os.dup2(fd, 1)
        os.dup2(fd, 2)
        os.close(fd)
    with tempfile.TemporaryDirectory(prefix='gm-vector-coherence-fixture-') as temp:
        online_snapshot(Path(temp))
    print(json.dumps({'suite': 'backup_coherence', 'required': list(REQUIRED), 'passed': list(REQUIRED),
                      'status': 'PASS'}), flush=True)


if __name__ == '__main__':
    main()
