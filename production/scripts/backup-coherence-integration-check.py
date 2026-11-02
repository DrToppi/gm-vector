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

spec = importlib.util.spec_from_file_location('coherence_backup', Path(__file__).with_name('backup.py'))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)


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
    finally:
        b.run = original
        original(['rm', '-f', name])


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


if __name__ == '__main__':
    main()
