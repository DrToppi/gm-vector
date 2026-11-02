"""Private synthetic recovery fixtures. No host installation/config is discovered."""
import base64
import contextlib
import datetime
import hashlib
import hmac
import http.cookiejar
import importlib.util
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import socket
import ssl
import struct
import subprocess
import tarfile
import time
import traceback
import uuid
from urllib.error import HTTPError
from urllib.request import Request, build_opener, HTTPSHandler, HTTPCookieProcessor, ProxyHandler, HTTPRedirectHandler

from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

ROOT = Path(__file__).resolve().parents[2]
ORIGINS = ('ab4abfee0bc6b8d16a6453fe3bae1958624af3d9',
           'c8c34ae211e185d2acbced0aead9549c4015c7d7',
           'e97971ddf5125b433f27e194b1be3144c3748d67')
CONTRACT = ('production/deploy/compose.yaml', 'production/deploy/install.py',
            'production/Dockerfile.db', 'production/Dockerfile.proxy',
            'production/scripts/database-roles.py', 'production/tls/nginx.files.conf')
TABLES = ('calendars', 'employees', 'projects', 'tasks', 'participation', 'assignments',
          'dependencies', 'time_entries', 'memberships', 'users', 'enrollments',
          'employee_invitations', 'outbox', 'directory_entries')
DERIVED = ('loads', 'warnings', 'provisional_years', 'calculation_state')


def module(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


backup = module(Path(__file__).with_name('backup.py'), 'fixture_backup')


def private(path, data):
    data = data if isinstance(data, bytes) else json.dumps(data).encode()
    with path.open('wb') as stream:
        os.chmod(path, 0o600)
        stream.write(data)


def evidence(path):
    if path:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        os.dup2(fd, 1)
        os.dup2(fd, 2)
        os.close(fd)


def command(argv, *, data=None, timeout=300, env=None):
    result = subprocess.run(argv, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, env=env)
    if result.returncode:
        raise RuntimeError('synthetic fixture subprocess failed')
    return result.stdout


def source_identity(source):
    source = Path(source).resolve()
    sha = command(['git', '-C', str(source), 'rev-parse', 'HEAD']).decode().strip()
    if not re.fullmatch('[0-9a-f]{40}', sha):
        raise ValueError('invalid fixture source identity')
    if command(['git', '-C', str(source), 'status', '--porcelain', '--untracked-files=no']).strip():
        raise ValueError('fixture source must be a clean committed checkout')
    return sha


def image_manifest(installer, overrides):
    defaults = installer.package()['images'] if not all(overrides.values()) else {}
    images = {}
    for kind in ('app', 'db', 'proxy'):
        reference = overrides.get(kind) or defaults[kind]
        actual = command(installer.docker() + ['image', 'inspect', '--format', '{{.Id}}', reference]).decode().strip()
        if not re.fullmatch('sha256:[0-9a-f]{64}', actual):
            raise ValueError('fixture image unavailable or invalid')
        images[kind] = actual
    return {'images': images}


def port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def otp(secret, step=None):
    if step is None:
        step = int(time.time()) // 30
    key = base64.b32decode(secret + '=' * ((-len(secret)) % 8))
    digest = hmac.new(key, struct.pack('>Q', step), hashlib.sha1).digest()
    offset = digest[-1] & 15
    return str((struct.unpack('>I', digest[offset:offset + 4])[0] & 0x7fffffff) % 1000000).zfill(6)


def literal(value):
    return "'" + str(value).replace("'", "''") + "'"


def b64(data):
    return base64.urlsafe_b64encode(data).decode().rstrip('=')


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise RuntimeError('fixture redirects are forbidden')


class Client:
    def __init__(self, fixture):
        self.fixture = fixture
        self.csrf = ''
        trust = ssl.create_default_context(cafile=str(fixture.state / 'tls/fullchain.pem'))
        self.opener = build_opener(ProxyHandler({}), NoRedirect(), HTTPSHandler(context=trust),
                                  HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, path, body=None, *, method=None, expected=(200,), raw=False):
        if not path.startswith('/') or path.startswith('//') or '\r' in path or '\n' in path:
            raise ValueError('invalid synthetic fixture path')
        request = Request(self.fixture.origin + '/api/v1' + path,
                          data=None if body is None else json.dumps(body).encode(), method=method,
                          headers={'Origin': self.fixture.origin, 'Content-Type': 'application/json',
                                   'X-CSRF-Token': self.csrf})
        try:
            response = self.opener.open(request, timeout=15)
        except HTTPError as error:
            response = error
        with response:
            status = response.status
            data = response.read((64 << 20) + 1)
        if len(data) > 64 << 20:
            raise AssertionError('synthetic fixture API response exceeds bound')
        if status not in expected:
            error = AssertionError('synthetic fixture API status did not match the contract')
            error.fixture_http_status = status
            error.fixture_resource = path.split('/')[1].split('?')[0]
            raise error
        if raw:
            return status, data
        result = json.loads(data) if data else None
        if path == '/auth/me' and status == 200:
            self.csrf = result['csrf']
        return result


class Fixture:
    """One fresh compose installation; cleanup targets only recorded own resources."""
    def __init__(self, root, source, images, *, source_sha=None):
        self.root = Path(root)
        self.source = Path(source).resolve()
        self.sha = source_sha or source_identity(self.source)
        self.installer = module(self.source / 'production/deploy/install.py', 'recovery_install_' + uuid.uuid4().hex)
        self.manifest = image_manifest(self.installer, images)
        self.state = self.root / 'state'
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.clients = []
        self.admin = None
        self.creds = None
        self.compose = None
        self.env = None
        self.support_private = rsa.generate_private_key(public_exponent=65537, key_size=3072)

    def initialize(self):
        # Unique /29 avoids the working installation and other fixture networks.
        subnet = '172.29.' + str(secrets.randbelow(220) + 20) + '.0/29'
        with contextlib.redirect_stdout(io.StringIO()):
            self.installer.initialize(self.state, port(), subnet)
        cfg = self.state / 'config'
        runtime = json.loads((cfg / 'config.json').read_bytes())
        self.origin = runtime['origin']
        runtime['planning_timezone'] = 'America/New_York'
        runtime['smtp_file'] = '/run/secrets/smtp.json'
        runtime['ldap_file'] = '/run/secrets/ldap.json'
        runtime['report_public_key_file'] = '/run/secrets/report-public-key.json'
        public = self.support_private.public_key().public_numbers()
        key = {'kty': 'RSA', 'n': b64(public.n.to_bytes(384, 'big')), 'e': 'AQAB',
               'alg': 'RSA-OAEP-256', 'use': 'enc'}
        key['kid'] = b64(hashlib.sha256(json.dumps({name: key[name] for name in ('e', 'kty', 'n')},
                                               separators=(',', ':')).encode()).digest())
        private(cfg / 'report-public-key.json', key)
        private(cfg / 'smtp.json', {'enabled': True, 'host': '127.0.0.1', 'port': 2525,
                                    'security': 'tls', 'from': 'sender@example.test',
                                    'username': 'synthetic', 'password': secrets.token_urlsafe(32),
                                    'ca_file': '/run/secrets/smtp-ca.pem'})
        private(cfg / 'smtp-ca.pem', (self.state / 'tls/fullchain.pem').read_bytes())
        private(cfg / 'ldap.json', {'enabled': False})
        private(cfg / 'config.json', runtime)
        self.compose, self.env = self.installer.context(self.state, self.manifest)
        with contextlib.redirect_stdout(io.StringIO()):
            self.installer.prepare(self.state, self.manifest)
        self.stop_worker()
        self.assert_release()
        self.admin = self.client()

    def client(self):
        client = Client(self)
        self.clients.append(client)
        return client

    def docker(self, args, data=None):
        return command(self.installer.docker() + args, data=data)

    def container(self, service):
        value = self.installer.run(self.compose + ['ps', '-q', service], self.env, capture=True)
        if not re.fullmatch('[0-9a-f]{12,64}', value):
            raise AssertionError('synthetic service is not uniquely identified')
        return value

    def sql(self, statement, database='planner'):
        return self.docker(['exec', '-i', self.container('db'), 'psql', '-X', '-qAt', '-v',
                            'ON_ERROR_STOP=1', '-U', 'planner', '-d', database], statement.encode()).decode().strip()

    def stop_worker(self):
        self.installer.run(self.compose + ['stop', 'worker'], self.env, capture=True)

    def stop_writers(self):
        self.installer.run(self.compose + ['stop', 'api', 'worker', 'proxy'], self.env, capture=True)
        assert not self.installer.run(self.compose + ['ps', '--status', 'running', '-q', 'api', 'worker', 'proxy'],
                                      self.env, capture=True), 'synthetic writers were not stopped'

    def assert_release(self):
        raw = self.docker(['exec', self.container('api'), '/app/planner', 'release-info'])
        assert json.loads(raw)['git_commit'] == self.sha, 'source and packaged runtime do not match'
        for service, kind in (('api', 'app'), ('worker', 'app'), ('db', 'db'), ('proxy', 'proxy')):
            if service == 'worker':
                cid = self.installer.run(self.compose + ['ps', '-aq', service], self.env, capture=True)
            else:
                cid = self.container(service)
            actual = self.docker(['inspect', '--format', '{{.Image}}', cid]).decode().strip()
            assert actual == self.manifest['images'][kind], 'actual synthetic image differs from pinned image'

    def seed(self):
        password = secrets.token_urlsafe(30)
        enrolled = self.admin.call('/auth/bootstrap', {'username': 'synthetic-recovery-admin',
                                  'name': 'Synthetic administrator', 'password': password})
        confirmed = self.admin.call('/auth/bootstrap/confirm', {'token': enrolled['enrollment_token'],
                                                               'code': otp(enrolled['secret'])})
        self.admin.call('/auth/me')
        self.creds = {'username': 'synthetic-recovery-admin', 'password': password,
                      'secret': enrolled['secret'], 'recovery': confirmed['recovery_codes']}
        # All three origin schemas support these common-denominator fields.
        sql = []
        for year in (2025, 2026, 2027):
            days = {}
            date = datetime.date(year, 1, 1)
            while date.year == year:
                days[str(date)] = {'working': date.weekday() < 5 and not (date.month == 1 and date.day == 1),
                                   'shortened': False}
                date += datetime.timedelta(days=1)
            encoded = json.dumps(days, separators=(',', ':'))
            sql.append("INSERT INTO calendars(id,year,title,source,checksum,days) VALUES(" +
                       ','.join((literal('synthetic-recovery-' + str(year)), str(year), literal('Synthetic year'),
                                 literal('isolated fixture; not official'), literal(hashlib.sha256(encoded.encode()).hexdigest()),
                                 literal(encoded))) + ');')
        for n in range(30):
            sql.append("INSERT INTO employees(id,name,daily_minutes) VALUES(" + literal('staff-' + str(n)) +
                       ',' + literal('Synthetic staff ' + str(n)) + ',480);')
        week = json.dumps({str(n): {'active': True, 'start': '09:00', 'end': '17:00', 'daily_minutes': 96}
                           for n in range(1, 6)})
        for p in range(5):
            pid = 'recovery-project-' + str(p)
            sql.append("INSERT INTO projects(id,title,start_date,duration_mode,calendar_id) VALUES(" + literal(pid) +
                       ',' + literal('Synthetic recovery ' + str(p)) + ",'2026-01-05','working','synthetic-recovery-2026');")
            sql.append("INSERT INTO tasks(project_id,id,title,status,legacy_actual_minutes) VALUES(" + literal(pid) +
                       ",'root','Synthetic milestone','todo',23);")
            for n in range(299):
                tid = 'task-' + str(n)
                sql.append("INSERT INTO tasks(project_id,id,parent_id,title,status,requested_start,planned_minutes,legacy_actual_minutes,sort_order) VALUES(" +
                           ','.join((literal(pid), literal(tid), "'root'", literal('Synthetic task ' + str(n)), "'todo'",
                                     "'2026-01-05'", '60', '17' if n == 0 else '0', str(n + 1))) + ');')
                sql.append("INSERT INTO assignments(project_id,task_id,employee_id,daily_minutes) VALUES(" +
                           ','.join((literal(pid), literal(tid), literal('staff-' + str(n % 30)), '96')) + ');')
            for n in range(30):
                sql.append("INSERT INTO participation(project_id,employee_id,weekdays,window_start,window_end,week) VALUES(" +
                           ','.join((literal(pid), literal('staff-' + str(n)), "'{1,2,3,4,5}'", "'09:00'", "'17:00'", literal(week))) + ');')
            sql.append("INSERT INTO dependencies VALUES(" + literal(pid) + ",'task-0','task-1','after',0);")
        # NULL identity and no longer assigned employee are intentional retained facts.
        sql.append("INSERT INTO time_entries(id,project_id,task_id,employee_id,work_date,minutes,comment) SELECT 'retained-'||n,'recovery-project-0','task-0',CASE WHEN n=1 THEN NULL ELSE 'staff-29' END,'2024-02-29',1,'Synthetic retained fact' FROM generate_series(1,1001)n;")
        self.sql('\n'.join(sql))
        for p in range(5):
            project = self.admin.call('/projects/recovery-project-' + str(p))['project']
            if 'calendar_versions' in project:
                project['calendar_versions'] = {'2027': 'synthetic-recovery-2027'}
            for key in DERIVED:
                project.pop(key, None)
            # Real save computes/persists dates rather than a synthetic DB-only oracle.
            self.admin.call('/projects/' + project['id'], {'project': project, 'override_reason': ''}, method='PUT')
        pending = self.admin.call('/users', {'username': 'synthetic-pending', 'name': 'Synthetic pending', 'admin': False}, expected=(201,))
        self.pending = self.admin.call('/auth/bootstrap', {'token': pending['enrollment_token'],
                                    'username': 'synthetic-pending', 'name': 'Synthetic pending',
                                    'password': secrets.token_urlsafe(30)})
        self.sql("INSERT INTO memberships VALUES('recovery-project-0'," + literal(pending['id']) + ",'viewer');")
        expired = self.admin.call('/users', {'username': 'synthetic-expired', 'name': 'Synthetic expired', 'admin': False}, expected=(201,))
        self.expired_token = expired['enrollment_token']
        self.sql('UPDATE enrollments SET expires_at=now()-interval \'1 second\' WHERE hash=' +
                 literal(hashlib.sha256(self.expired_token.encode()).hexdigest()) + ';')
        self.sql("UPDATE employees SET provider='synthetic-directory',external_id='pending-staff',email='staff@example.test' WHERE id='staff-29';")
        self.admin.call('/directory/sync', {'provider': 'synthetic-directory', 'entries': [
            {'id': 'pending-staff', 'name': 'Synthetic directory staff', 'email': 'staff@example.test',
             'username': 'directory-staff', 'active': True, 'groups': ['synthetic-group']}]})
        employee = self.sql("SELECT id FROM employees WHERE provider='synthetic-directory' AND external_id='pending-staff'")
        self.invitation = self.admin.call('/employees/' + employee + '/invite', {'id': str(uuid.uuid4()), 'reissue': False}, expected=(201,))
        self.sql("UPDATE outbox SET ready_at=now()+interval '2 hours' WHERE provider='smtp_invitation';")
        raw = json.loads(self.sql('SELECT payload::text FROM outbox WHERE id=' + literal(self.invitation['id'])))
        sealed = base64.b64decode(raw['secret'])
        self.invitation_token = AESGCM((self.state / 'config/encryption.key').read_bytes()).decrypt(
            sealed[:12], sealed[12:], b'GMA|Planner:v1').decode()
        assert self.sql('SELECT token_hash FROM employee_invitations WHERE id=' + literal(self.invitation['id'])) == hashlib.sha256(self.invitation_token.encode()).hexdigest()
        self.assert_no_send()
        self.report = self.make_report()
        saved = self.admin.call('/diagnostics/reports', {'encrypted': self.report}, expected=(201,))
        self.report_id = saved['id']
        self.roles()

    def make_report(self):
        key = json.loads((self.state / 'config/report-public-key.json').read_bytes())
        protected = b64(json.dumps({'alg': 'RSA-OAEP-256', 'enc': 'A256GCM', 'kid': key['kid'],
                                   'typ': 'gma-bug-report+jwe', 'cty': 'application/json'}, separators=(',', ':')).encode())
        cek, nonce = secrets.token_bytes(32), secrets.token_bytes(12)
        encrypted = AESGCM(cek).encrypt(nonce, b'{"synthetic":true}', protected.encode())
        wrapped = self.support_private.public_key().encrypt(cek, padding.OAEP(mgf=padding.MGF1(hashes.SHA256()),
                                                                           algorithm=hashes.SHA256(), label=None))
        return '.'.join((protected, b64(wrapped), b64(nonce), b64(encrypted[:-16]), b64(encrypted[-16:])))

    def assert_no_send(self):
        assert self.sql("SELECT coalesce(sum(attempts),0) FROM outbox WHERE provider IN ('smtp','smtp_invitation')") == '0'
        assert self.sql("SELECT count(*) FROM outbox WHERE state IN ('sending','sent','uncertain')") == '0'

    def roles(self):
        flags = self.sql("SELECT rolname,rolsuper,rolcreatedb,rolcreaterole,rolreplication FROM pg_roles WHERE rolname IN ('planner_runtime','planner_migrator') ORDER BY rolname")
        assert flags == 'planner_migrator|f|f|f|f\nplanner_runtime|f|f|f|f', 'production role flags changed'
        result = self.docker(['exec', self.container('db'), 'psql', '-X', '-qAt', '-U', 'planner', '-d', 'planner',
                             '-c', "SET ROLE planner_runtime; SELECT count(*) FROM projects;"])
        assert result.decode().strip() == '5'
        denied = subprocess.run(self.installer.docker() + ['exec', self.container('db'), 'psql', '-X', '-qAt',
                  '-v', 'ON_ERROR_STOP=1', '-U', 'planner', '-d', 'planner', '-c',
                  'SET ROLE planner_runtime; CREATE TABLE synthetic_runtime_ddl_forbidden(id int);'],
                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
        assert denied.returncode != 0, 'runtime unexpectedly acquired DDL privileges'
        assert self.sql("SELECT to_regclass('public.synthetic_runtime_ddl_forbidden') IS NULL") == 't'

    def fingerprint(self, columns=None):
        projection = {} if columns is None else columns
        result = {}
        for table in TABLES:
            if columns is None:
                projection[table] = self.sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=" + literal(table) + ' ORDER BY ordinal_position').splitlines()
            names = projection[table]
            assert names and all(re.fullmatch('[a-z_][a-z_0-9]*', n) for n in names)
            predicate = ' WHERE expires_at>now()' if table == 'enrollments' else ''
            raw = self.sql('SELECT row_to_json(r)::text FROM (SELECT ' + ','.join('"' + n + '"' for n in names) + ' FROM ' + table + predicate + ') r ORDER BY row_to_json(r)::text')
            result[table] = hashlib.sha256(raw.encode()).hexdigest()
        return projection, result

    def schema_state(self):
        return {
            'versions': self.sql('SELECT version,checksum FROM schema_versions ORDER BY version').splitlines(),
            'columns': self.sql("SELECT table_name,column_name,data_type,is_nullable,coalesce(column_default,'') FROM information_schema.columns WHERE table_schema='public' ORDER BY table_name,ordinal_position").splitlines(),
            'constraints': self.sql("SELECT c.relname,k.conname,pg_get_constraintdef(k.oid) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' ORDER BY c.relname,k.conname").splitlines(),
        }

    def candidate_schema_versions(self, source=None):
        directory = (Path(source) if source is not None else self.source) / 'production/internal/planner'
        files = [directory / 'schema.sql'] + sorted(directory.glob('schema-[0-9][0-9][0-9].sql'))
        return [str(n + 1) + '|' + hashlib.sha256(path.read_bytes()).hexdigest()
                for n, path in enumerate(files)]

    def prove_migrations(self, candidate, manifest, schema, expected, columns):
        """Actual migration contenders, cancellation/rollback and repeated apply."""
        self.stop_writers()
        installer = module(Path(candidate) / 'production/deploy/install.py', 'migration_proof_' + secrets.token_hex(6))
        compose, env = installer.context(self.state, manifest)
        suffix = secrets.token_hex(8)
        initial_name, inspection_name = 'recovery-migration-initial-' + suffix, 'recovery-migration-inspect-' + suffix
        blockers, migrations, names = [], [], []
        pending = len(schema['versions']) < len(self.candidate_schema_versions(candidate))

        def barrier(sql, timeout=20):
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                result = self.sql(sql)
                if result:
                    return result
            raise AssertionError('actual migration lock barrier was not reached')

        def psql_blocker(application, sql):
            child = subprocess.Popen(self.installer.docker() + ['exec', '-i', self.container('db'), 'psql', '-X',
                '-qAt', '-v', 'ON_ERROR_STOP=1', '-U', 'planner', '-d', 'planner'], stdin=subprocess.PIPE,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            blockers.append(child)
            child.stdin.write(("BEGIN; SET LOCAL application_name=" + literal(application) + '; ' + sql + ';\n').encode())
            child.stdin.flush()
            return child

        def release(child):
            child.stdin.write(b'ROLLBACK;\n\\q\n'); child.stdin.flush(); child.stdin.close()
            assert child.wait(timeout=5) == 0

        def migrate():
            name = 'gm-vector-migration-' + secrets.token_hex(8)
            names.append(name)
            child = subprocess.Popen(compose + ['run', '--rm', '--no-deps', '--name', name, 'migrate'], env=env,
                                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            migrations.append(child)
            return child

        advisory_wait = "SELECT a.pid FROM pg_stat_activity a WHERE a.datname='planner' AND a.usename='planner_migrator' AND a.wait_event_type='Lock' AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.locktype='advisory' AND NOT l.granted)"
        ddl_wait = "SELECT a.pid FROM pg_stat_activity a WHERE a.datname='planner' AND a.usename='planner_migrator' AND a.wait_event_type='Lock' AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation='public.tasks'::regclass AND l.mode='AccessExclusiveLock' AND NOT l.granted) AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation='public.projects'::regclass AND l.mode='AccessExclusiveLock' AND l.granted)"
        try:
            initial = psql_blocker(initial_name, 'LOCK TABLE tasks IN ACCESS SHARE MODE' if pending else 'SELECT pg_advisory_xact_lock(789501)')
            barrier("SELECT pid FROM pg_stat_activity WHERE application_name=" + literal(initial_name) + " AND state='idle in transaction'")
            first = migrate()
            first_pid = barrier(ddl_wait if pending else advisory_wait)
            assert re.fullmatch('[0-9]+', first_pid)
            # Queue inspection before the second contender, so cancellation of
            # the first allows an exact old-schema/data assertion before retry.
            if pending:
                inspection = psql_blocker(inspection_name, 'SELECT pg_advisory_xact_lock(789501)')
                barrier("SELECT pid FROM pg_stat_activity WHERE application_name=" + literal(inspection_name) + " AND wait_event_type='Lock'")
            else:
                inspection = initial
            second = migrate()
            second_pid = barrier(advisory_wait + ' AND a.pid<>' + first_pid)
            assert re.fullmatch('[0-9]+', second_pid)
            assert self.sql("SELECT pg_terminate_backend(pid,1000) FROM pg_stat_activity WHERE pid=" + first_pid + " AND datname='planner' AND usename='planner_migrator'") == 't'
            assert first.wait(timeout=15) != 0
            barrier("SELECT pid FROM pg_stat_activity WHERE application_name=" + literal(inspection_name if pending else initial_name) + " AND state='idle in transaction'")
            assert self.schema_state() == schema, 'cancelled migration left a partial schema/checksum state'
            assert self.fingerprint(columns)[1] == expected, 'cancelled migration changed authoritative data'
            release(inspection)
            if pending:
                assert barrier(ddl_wait + ' AND a.pid=' + second_pid) == second_pid
                release(initial)
            assert second.wait(timeout=30) == 0
            migrated = self.schema_state()
            assert migrated['versions'] == self.candidate_schema_versions(candidate)
            assert migrated['versions'][:len(schema['versions'])] == schema['versions']
            assert self.fingerprint(columns)[1] == expected
            command(compose + ['run', '--rm', '--no-deps', 'migrate'], timeout=60, env=env)
            assert self.schema_state() == migrated, 'repeated migration changed schema/checksums'
            return {'concurrent_serialization': True, 'cancelled_migration_unchanged': True,
                    'repeated_schema_unchanged': True, 'interruption_phase':
                    'after_projects_ddl_before_tasks_ddl' if pending else 'advisory_wait_no_pending_schema'}
        finally:
            # Every name/PID belongs to this private fixture. No caller supplied
            # DB or host daemon resources are discovered or terminated here.
            self.sql("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='planner' AND usename='planner' AND application_name IN (" + literal(initial_name) + ',' + literal(inspection_name) + ');')
            for child in blockers:
                if not child.stdin.closed:
                    try: child.stdin.close()
                    except BrokenPipeError: pass
                try: child.wait(timeout=2)
                except subprocess.TimeoutExpired: child.kill(); child.wait(timeout=2)
            for name in names:
                subprocess.run(installer.docker() + ['rm', '-f', name], stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, timeout=10)
            for child in migrations:
                try: child.wait(timeout=2)
                except subprocess.TimeoutExpired: child.kill(); child.wait(timeout=2)

    def active_files(self):
        """Fixed compose mappings; never treat staging files as active secrets."""
        values = backup.collect_secrets(self.state / 'config')
        mappings = {}
        for name in ('config.json', 'encryption.key', 'report-public-key.json', 'smtp.json', 'smtp-ca.pem',
                     'ldap.json', 'ldap-ca.pem', 'updater.token'):
            if 'secrets/' + name in values:
                mappings['secrets/' + name] = ('api', '/run/secrets/' + name)
        mappings['secrets/migration-config.json'] = ('migrate', '/run/migration/migration-config.json')
        mappings['secrets/database.password'] = ('db', '/run/db-secrets/password')
        for name, (service, source) in mappings.items():
            if name not in values:
                continue
            reader = None
            if service == 'migrate':
                # Production runs migrate with --rm: its secret volume persists,
                # but there is deliberately no stopped migrate container to copy.
                project = json.loads((self.state / 'installation.json').read_bytes())['project']
                volume = project + '_migration_secrets'
                info = json.loads(self.docker(['volume', 'inspect', volume]))
                assert len(info) == 1 and info[0]['Name'] == volume
                labels = info[0].get('Labels') or {}
                assert labels.get('com.docker.compose.project') == project
                assert labels.get('com.docker.compose.volume') == 'migration_secrets'
                reader = self.docker(['create', '--network', 'none', '--read-only', '--cap-drop', 'ALL',
                    '--security-opt', 'no-new-privileges:true', '--user', '65532:65532', '--memory', '32m',
                    '--pids-limit', '32', '--mount', 'type=volume,source=' + volume + ',target=/active,readonly',
                    '--entrypoint', '/bin/true', self.manifest['images']['proxy']]).decode().strip()
                cid, source = reader, '/active/migration-config.json'
            else:
                cid = self.installer.run(self.compose + ['ps', '-aq', service], self.env, capture=True)
            assert re.fullmatch('[0-9a-f]{12,64}', cid), 'active secret container unavailable'
            try:
                payload = self.docker(['cp', cid + ':' + source, '-'])
            finally:
                if reader:
                    self.docker(['rm', reader])
            with tarfile.open(fileobj=io.BytesIO(payload), mode='r:') as tar:
                members = tar.getmembers()
                assert len(members) == 1 and members[0].isfile() and members[0].size <= backup.MAX_SECRET
                active = tar.extractfile(members[0]).read()
            if active != values[name]:
                raise ValueError('staged configuration does not match active secret volumes')
        return {name: hashlib.sha256(value).hexdigest() for name, value in values.items()}

    def artifacts(self):
        files = {}
        for name in ('installation.json', 'nginx.conf', 'deployment.json', 'https.json', 'tls/fullchain.pem', 'tls/privkey.pem'):
            path = self.state / name
            if path.exists():
                if path.is_symlink() or not path.is_file() or path.stat().st_size > backup.MAX_SECRET:
                    raise ValueError('unsafe operator artifact')
                files[name] = path.read_bytes()
        payload = self.docker(['cp', self.container('api') + ':/var/lib/planner-reports/.', '-'])
        if len(payload) > (65 << 20):
            raise ValueError('operator report artifacts exceed bound')
        with tarfile.open(fileobj=io.BytesIO(payload), mode='r:') as tar:
            for member in tar:
                if member.isdir():
                    continue
                path = PurePosixPath(member.name)
                basename = path.name
                if not member.isfile() or path.is_absolute() or len(path.parts) != 1 or '..' in path.parts or (basename != '.lock' and not re.fullmatch(r'[0-9a-f]{64}\.gma-bug', basename)):
                    raise ValueError('unsafe report artifact')
                if member.size > 256 << 10:
                    raise ValueError('oversized report artifact')
                if basename != '.lock':
                    if 'reports/' + basename in files or sum(n.startswith('reports/') for n in files) >= 256:
                        raise ValueError('duplicate or excess report artifacts')
                    files['reports/' + basename] = tar.extractfile(member).read()
        assert files.get('reports/' + self.report_id + '.gma-bug') == self.report.encode()
        return files

    def snapshot(self, destination):
        before = self.active_files()
        artifact = self.artifacts()
        self.stop_worker()
        start = datetime.datetime.now(datetime.timezone.utc).isoformat()
        args = type('BackupArgs', (), {'config': str(self.state / 'config'), 'destination': str(destination),
                                     'key': str(self.root / 'backup.key'), 'container': self.container('db')})()
        with contextlib.redirect_stdout(io.StringIO()):
            archive = backup.backup(args)
        assert self.active_files() == before, 'configuration changed during frozen capture window'
        end = datetime.datetime.now(datetime.timezone.utc).isoformat()
        return args, archive, artifact, (start, end)

    def restore(self, args, archive, artifacts, *, installer=None, manifest=None, sha=None):
        self.stop_writers()
        self.installer.run(self.compose + ['down', '--volumes', '--remove-orphans'], self.env, capture=True)
        values, meta, complete = backup.read_archive(args, archive)
        assert complete
        for name, value in values.items():
            if name.startswith('secrets/'):
                private(self.state / 'config' / name.removeprefix('secrets/'), value)
        for name, value in artifacts.items():
            if name.startswith('reports/'):
                continue
            path = self.state / name
            path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            private(path, value)
            if name == 'nginx.conf':
                path.chmod(0o644)
        if installer is not None:
            self.installer = installer
            self.source = Path(installer.ROOT).parents[1]
            self.manifest = manifest
            self.sha = sha
        self.compose, self.env = self.installer.context(self.state, self.manifest)
        self.installer.run(self.compose + ['run', '--rm', '--no-deps', 'init-secrets'], self.env, capture=True)
        self.installer.run(self.compose + ['up', '-d', '--no-deps', '--wait', 'db'], self.env, capture=True)
        self.docker(['exec', '-i', self.container('db'), 'pg_restore', '-U', 'planner', '-d', 'planner',
                     '--no-owner', '--no-acl', '--exit-on-error'], values['database.dump'])
        assert backup.counts(self.container('db')) == meta['counts']
        # Mandatory before any successful readiness/auth acceptance.
        backup.verify_restored_secrets(self.container('db'), values['secrets/encryption.key'])
        with contextlib.redirect_stdout(io.StringIO()):
            self.installer.prepare(self.state, self.manifest)
        self.assert_release()
        self.stop_worker()
        self.restore_reports(artifacts)
        self.admin = self.client()

    def restore_reports(self, artifacts):
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode='w') as tar:
            for name, value in artifacts.items():
                if not name.startswith('reports/'):
                    continue
                item = tarfile.TarInfo(name.removeprefix('reports/'))
                item.size = len(value)
                item.mode = 0o600
                item.uid = item.gid = 65532
                tar.addfile(item, io.BytesIO(value))
        self.docker(['cp', '-', self.container('api') + ':/var/lib/planner-reports'], buffer.getvalue())
        restored = self.artifacts()
        for name, value in artifacts.items():
            if name != 'deployment.json':
                assert restored[name] == value, 'operator artifact changed during restore'
        deployment = json.loads(restored['deployment.json'])
        assert deployment['commit'] == self.sha and deployment['images'] == self.manifest['images']

    def verify_usable(self, expected, columns):
        assert self.fingerprint(columns)[1] == expected, 'restored authoritative data differs'
        last = int(self.sql('SELECT last_totp FROM users WHERE username=' + literal(self.creds['username'])))
        step = max(int(time.time()) // 30, last + 1)
        assert step <= int(time.time()) // 30 + 1, 'fixture exhausted legitimate TOTP skew window'
        code = otp(self.creds['secret'], step)
        self.admin.call('/auth/login', {**{k: self.creds[k] for k in ('username', 'password')}, 'code': code})
        me = self.admin.call('/auth/me')
        assert me['user']['admin'] is True
        self.client().call('/auth/login', {'username': self.creds['username'], 'password': self.creds['password'], 'code': code}, expected=(401,))
        resumed = self.admin.call('/auth/bootstrap/resume', {'token': self.pending['enrollment_token']})
        assert resumed['secret'] == self.pending['secret']
        member = self.client()
        member.call('/auth/bootstrap/confirm', {'token': self.pending['enrollment_token'], 'code': otp(self.pending['secret'])})
        member.call('/auth/me')
        assert member.call('/projects/recovery-project-0')['role'] == 'viewer'
        member.call('/users', expected=(403,))
        project = self.admin.call('/projects/recovery-project-0')['project']
        for key in DERIVED:
            project.pop(key, None)
        member.call('/projects/recovery-project-0', {'project': project, 'override_reason': ''}, method='PUT', expected=(403,))
        self.client().call('/auth/bootstrap/resume', {'token': self.pending['enrollment_token']}, expected=(403,))
        self.client().call('/auth/bootstrap', {'token': self.expired_token, 'username': 'synthetic-expired',
                          'name': 'Synthetic expired', 'password': secrets.token_urlsafe(30)}, expected=(403,))
        invitee = self.client()
        enrolled = invitee.call('/auth/bootstrap', {'token': self.invitation_token,
                                'username': self.invitation['username'], 'name': 'Synthetic invited employee',
                                'password': secrets.token_urlsafe(30)})
        invitee.call('/auth/bootstrap/confirm', {'token': enrolled['enrollment_token'], 'code': otp(enrolled['secret'])})
        invitee.call('/auth/me')
        self.client().call('/auth/bootstrap', {'token': self.invitation_token,
                          'username': self.invitation['username'], 'name': 'Synthetic invited employee',
                          'password': secrets.token_urlsafe(30)}, expected=(403,))
        assert len(self.admin.call('/directory/people?provider=synthetic-directory')) == 1
        self.admin.call('/directory/ldap/sync', {}, expected=(400, 422))
        project['brief'] = 'Synthetic post-restore acknowledged write'
        written = self.admin.call('/projects/recovery-project-0', {'project': project, 'override_reason': ''}, method='PUT')
        assert written['brief'] == project['brief']
        self.admin.call('/projects/recovery-project-0/export', raw=True)
        # Current candidates additionally prove retained NULL identity/history provenance.
        if 'calendar_versions' in project:
            page = self.admin.call('/projects/recovery-project-0/time-history?task_id=root&limit=100')
            assert page['total_count'] == 1001
        self.roles()
        self.assert_no_send()
        self.restart_and_check_brief(project['brief'])

    def verify_new_features(self):
        project = self.admin.call('/projects/recovery-project-0')['project']
        assert 'calendar_versions' in project
        project['calendar_versions'] = {'2027': 'synthetic-recovery-2027'}
        for task in project['tasks']:
            if task['id'] in ('task-297', 'task-298'):
                task['requested_start'] = '2026-12-31'
                task['planned_minutes'] = 192 if task['id'] == 'task-297' else 96
                task['assignments'] = [{'employee_id': 'staff-0', 'daily_minutes': 96}]
        project['links'].append({'from': 'task-297', 'to': 'task-298', 'mode': 'after', 'days': 0})
        for key in DERIVED:
            project.pop(key, None)
        saved = self.admin.call('/projects/recovery-project-0', {'project': project, 'override_reason': ''}, method='PUT')
        first = next(t for t in saved['tasks'] if t['id'] == 'task-297')
        second = next(t for t in saved['tasks'] if t['id'] == 'task-298')
        assert (first['start'], first['end'], second['start']) == ('2026-12-31', '2027-01-04', '2027-01-05')
        assert saved['calendar_versions']['2027'] == 'synthetic-recovery-2027'
        self.restart_and_check_brief(project['brief'])

    def restart_and_check_brief(self, brief):
        self.installer.run(self.compose + ['restart', 'api', 'worker', 'proxy'], self.env, capture=True)
        self.installer.wait_web(self.state, self.compose, self.env)
        assert self.admin.call('/projects/recovery-project-0')['project']['brief'] == brief
        self.assert_no_send()

    def prove_ready_is_not_decryptability(self):
        """Only this disposable installation intentionally starts with a bad key."""
        path = self.state / 'config/encryption.key'
        correct = path.read_bytes()
        wrong = secrets.token_bytes(32)
        recovery_code = self.creds['recovery'][0]
        assert self.sql('SELECT ' + literal(hashlib.sha256(recovery_code.encode()).hexdigest()) +
                        '=ANY(recovery_hashes) FROM users WHERE username=' + literal(self.creds['username'])) == 't'
        self.stop_worker()
        try:
            private(path, wrong)
            self.installer.run(self.compose + ['run', '--rm', '--no-deps', 'init-secrets'], self.env, capture=True)
            self.installer.run(self.compose + ['restart', 'api'], self.env, capture=True)
            self.installer.wait_web(self.state, self.compose, self.env)
            self.client().call('/auth/login', {'username': self.creds['username'], 'password': self.creds['password'],
                               'code': recovery_code}, expected=(401,))
            try:
                backup.verify_restored_secrets(self.container('db'), wrong)
            except ValueError:
                pass
            else:
                raise AssertionError('ready API with unusable application key passed sealed-field verification')
        finally:
            private(path, correct)
            self.installer.run(self.compose + ['run', '--rm', '--no-deps', 'init-secrets'], self.env, capture=True)
            self.installer.run(self.compose + ['restart', 'api'], self.env, capture=True)
            self.installer.wait_web(self.state, self.compose, self.env)
        restored = self.client()
        restored.call('/auth/login', {'username': self.creds['username'], 'password': self.creds['password'],
                      'code': recovery_code})
        restored.call('/auth/me')
        self.client().call('/auth/login', {'username': self.creds['username'], 'password': self.creds['password'],
                          'code': recovery_code}, expected=(401,))
        self.assert_no_send()

    def cleanup(self):
        if self.compose is not None:
            self.installer.run(self.compose + ['down', '--volumes', '--remove-orphans'], self.env, capture=True)


def image_arguments(parser, prefix=''):
    for kind in ('app', 'db', 'proxy'):
        parser.add_argument('--' + prefix + kind + '-image')


def images(args, prefix=''):
    return {kind: getattr(args, (prefix + kind + '_image').replace('-', '_')) for kind in ('app', 'db', 'proxy')}


def receipt(scenario, **values):
    print(json.dumps({'scenario': scenario, 'status': 'PASS', **values}), flush=True)


def failure(suite, error):
    locations = [{'source': Path(frame.filename).name, 'line': frame.lineno}
                 for frame in traceback.extract_tb(error.__traceback__)[-5:]]
    value = {'suite': suite, 'status': 'FAIL', 'error_class': type(error).__name__, 'locations': locations}
    if hasattr(error, 'fixture_http_status'):
        value.update(http_status=error.fixture_http_status, resource=error.fixture_resource)
    print(json.dumps(value), flush=True)
