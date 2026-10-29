"""Host-only source update engine. Never imported into the HTTP application."""
import contextlib, datetime, fcntl, hashlib, importlib.util, io, json, os, re, secrets, shutil, subprocess, tempfile, threading, types, uuid
from pathlib import Path

REPOSITORY = 'https://github.com/DrToppi/gm-vector.git'
BUSY = {'checking', 'updating', 'recovering'}
# Infrastructure and privileged installer contracts are deliberately outside app updates.
CONTRACT = ('production/deploy/compose.yaml', 'production/deploy/install.py',
            'production/Dockerfile.db', 'production/Dockerfile.proxy',
            'production/scripts/database-roles.py', 'production/tls/nginx.files.conf')


def module(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec); spec.loader.exec_module(value)
    return value


def atomic(path, value):
    path = Path(path); tmp = path.with_name('.'+path.name+'-'+uuid.uuid4().hex)
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, 'w') as stream:
            json.dump(value, stream); stream.flush(); os.fsync(stream.fileno())
        os.replace(tmp, path)
        d = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try: os.fsync(d)
        finally: os.close(d)
    finally:
        if tmp.exists(): tmp.unlink()


def reference(value):
    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._/-]{0,120}', value):
        raise ValueError('invalid_ref')
    if '..' in value or '//' in value or value.endswith(('/', '.', '.lock')):
        raise ValueError('invalid_ref')
    return value


def private_tree(path):
    path = Path(path).absolute()
    if any(p.is_symlink() for p in (path, *path.parents)):
        raise ValueError('unsafe_state')
    if not path.is_dir() or path.stat().st_uid != os.getuid() or path.stat().st_mode & 0o077:
        raise ValueError('unsafe_state')
    return path


class Engine:
    def __init__(self, state, source):
        self.state = private_tree(state); self.source = Path(source).resolve()
        self.root = self.state/'updater'; self.root.mkdir(mode=0o700, exist_ok=True)
        private_tree(self.root)
        self.mutex = threading.RLock(); self.running = False
        self.lock = os.open(self.root/'engine.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.installer = module(self.source/'production/deploy/install.py', 'update_installer')
        self.backup = module(self.source/'production/scripts/backup.py', 'update_backup')
        self.key = self.root/'backup.key'
        self.record = json.loads((self.root/'operation.json').read_text()) if (self.root/'operation.json').exists() else {'state':'idle','phase':'complete'}
        if self.record['state'] in BUSY:
            if self.record.get('writers_stopped'):
                try: self.stop_writers()
                except BaseException: self.record['stop_unconfirmed']=True
            self.record['state'] = 'recovery_required' if self.record.get('writers_stopped') else 'failed'
            self.save()

    def save(self): atomic(self.root/'operation.json', self.record)

    def status(self):
        with self.mutex:
            deployment = json.loads((self.state/'deployment.json').read_text())
            return {'enabled':True, 'state':self.record['state'], 'phase':self.record.get('phase','complete'),
                    'message':'', 'current_commit':deployment['commit'],
                    'candidate_commit':self.record.get('commit',''),
                    'operation_id':self.record.get('id',''),
                    'recovery_available':bool(self.record.get('writers_stopped') and not self.record.get('stop_unconfirmed'))}

    def phase(self, name):
        with self.mutex: self.record['phase'] = name; self.save()

    def command(self, args, cwd=None, env=None, data=None, timeout=600):
        # Never echo subprocess output: build scripts and DB tools can emit credentials.
        capture = 'rev-parse' in args or ('image' in args and 'inspect' in args)
        p = subprocess.run(args, cwd=cwd, env=env, input=data, stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL, timeout=timeout)
        if p.returncode: raise RuntimeError('operation_failed')
        if p.stdout is not None and len(p.stdout)>65536: raise RuntimeError('oversized_output')
        return (p.stdout or b'').decode().strip()

    def manifest(self):
        value = json.loads((self.state/'deployment.json').read_text())
        if not re.fullmatch('[a-f0-9]{40}',value.get('commit','')): raise ValueError('deployment_unidentified')
        if set(value.get('images',{})) != {'app','db','proxy'} or any(not re.fullmatch('sha256:[a-f0-9]{64}',x) for x in value['images'].values()):
            raise ValueError('deployment_unidentified')
        return value

    def checkout(self, ref):
        ref = reference(ref)
        dest = self.root/'releases'/uuid.uuid4().hex; dest.mkdir(mode=0o700, parents=True)
        env = {**os.environ, 'GIT_TERMINAL_PROMPT':'0', 'GIT_CONFIG_NOSYSTEM':'1'}
        git = ['git','-c','core.hooksPath=/dev/null','-c','protocol.file.allow=never','-c','credential.helper=']
        self.command(git+['init','--quiet',str(dest)],env=env)
        self.command(git+['-C',str(dest),'fetch','--no-recurse-submodules',REPOSITORY,ref],env=env)
        commit = self.command(git+['-C',str(dest),'rev-parse','FETCH_HEAD^{commit}'],env=env)
        if not re.fullmatch('[a-f0-9]{40}', commit): raise ValueError('invalid_commit')
        self.command(git+['-C',str(dest),'checkout','--detach','--quiet',commit],env=env)
        for path in dest.rglob('*'):
            if '.git' not in path.relative_to(dest).parts and path.is_symlink(): raise ValueError('source_symlink')
        return dest, commit

    def submit(self, action, data):
        with self.mutex:
            if self.running or self.record['state'] in BUSY: raise BlockingIOError('busy')
            if action == 'check':
                reference(data.get('ref'))
                if self.record.get('writers_stopped'): raise BlockingIOError('recovery_required')
                self.record = {'id':uuid.uuid4().hex,'state':'checking','phase':'queued','ref':data['ref']}
            elif action == 'apply':
                if self.record['state'] != 'ready' or data.get('commit') != self.record.get('commit') or data.get('confirmation') != self.record.get('commit'):
                    raise BlockingIOError('candidate_changed')
                self.record['state'] = 'updating'; self.record['phase'] = 'queued'
            elif action == 'recover':
                if not self.record.get('writers_stopped') or data.get('operation_id') != self.record.get('id') or data.get('confirmation') != self.record.get('id'):
                    raise BlockingIOError('recovery_mismatch')
                self.record['state'] = 'recovering'; self.record['phase'] = 'restore'
            else: raise ValueError('unknown_action')
            self.save(); self.running = True
            threading.Thread(target=self.execute, args=(action,),daemon=True).start()
            return self.status()

    def execute(self, action):
        try:
            getattr(self, action)()
        except BaseException:
            if self.record.get('writers_stopped'):
                try: self.stop_writers()
                except BaseException:
                    self.record['stop_unconfirmed']=True
            with self.mutex:
                self.record['state'] = 'recovery_required' if self.record.get('writers_stopped') else 'failed'
                self.save()
        finally:
            with self.mutex: self.running = False

    def compatible(self, old, candidate):
        for name in CONTRACT:
            a, b = old/name, candidate/name
            if not a.is_file() or not b.is_file() or a.read_bytes() != b.read_bytes():
                raise ValueError('infrastructure_change_requires_operator')

    def check(self):
        self.phase('fetch'); previous = self.manifest()
        old, sha = self.checkout(previous['commit'])
        if sha != previous['commit']: raise ValueError('baseline_changed')
        candidate, commit = self.checkout(self.record['ref'])
        self.command(['git','-C',str(candidate),'merge-base','--is-ancestor',previous['commit'],commit])
        self.compatible(old,candidate)
        if shutil.disk_usage(self.root).free < 2*1024**3: raise RuntimeError('insufficient_disk')
        self.record.update(source=str(candidate),previous_source=str(old),commit=commit,previous=previous)
        self.assert_current()
        self.save(); self.phase('build')
        self.command(['python3','production/scripts/build.py'],cwd=candidate,timeout=1800)
        tag = 'gm-vector:update-'+self.record['id']
        self.command(self.installer.docker()+['build','-t',tag,'production'],cwd=candidate,timeout=1800)
        image = self.command(self.installer.docker()+['image','inspect','--format','{{.Id}}',tag])
        if not re.fullmatch('sha256:[a-f0-9]{64}',image): raise ValueError('invalid_image')
        self.record['images'] = {**previous['images'],'app':image}
        self.phase('preflight')
        # Candidate code and migrations are exercised on an isolated restored clone.
        # Initial rehearsal is online; final backup is taken again after stopping writers.
        self.snapshot('rehearsal')
        self.rehearse()
        self.record['state'] = 'ready'; self.phase('complete')

    def context(self, previous=False):
        images = self.record['previous']['images'] if previous else self.record['images']
        install = module(Path(self.record['previous_source'])/'production/deploy/install.py','pinned_update_installer')
        compose, env = install.context(self.state, {'images':images})
        return install, compose, env

    def assert_current(self):
        install, compose, env = self.context(True)
        for service, name in (('api','app'),('worker','app'),('db','db'),('proxy','proxy')):
            container=install.run(compose+['ps','-q',service],env,capture=True)
            if not container or '\n' in container: raise ValueError('installation_not_running')
            actual=install.run(install.docker()+['inspect','--format','{{.Image}}',container],capture=True)
            if actual != self.record['previous']['images'][name]: raise ValueError('deployment_drift')

    def snapshot(self, label):
        install, compose, env = self.context(True)
        db = install.run(compose+['ps','-q','db'],env,capture=True)
        if not db or '\n' in db: raise ValueError('database_unidentified')
        self.record['database'] = db
        dest = self.root/'operations'/self.record['id']/label
        dest.mkdir(mode=0o700,parents=True,exist_ok=False)
        args = types.SimpleNamespace(config=str(self.state/'config'),destination=str(dest),key=str(self.key),container=db)
        with contextlib.redirect_stdout(io.StringIO()): archive = self.backup.backup(args)
        self.record[label] = str(archive); self.save()

    def archive(self, label):
        args = types.SimpleNamespace(key=str(self.key))
        values, meta, complete = self.backup.read_archive(args,self.record[label])
        if not complete or values['secrets/instance.id'].strip() != (self.state/'config/instance.id').read_bytes().strip():
            raise ValueError('backup_identity_mismatch')
        return values, meta

    def rehearse(self):
        values, meta = self.archive('rehearsal')
        name = 'gm-vector-update-check-'+self.record['id']
        b = self.backup
        try:
            b.run(['run','-d','--name',name,'--network','none','--memory','1g','--cpus','2','--pids-limit','256','--tmpfs','/var/lib/postgresql:size=768m',
                   '-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_USER=planner','-e','POSTGRES_DB=planner',self.record['previous']['images']['db']])
            import time
            for attempt in range(60):
                try: b.run(['exec',name,'pg_isready','-U','planner']); break
                except RuntimeError: time.sleep(.25)
            else: raise RuntimeError('clone_unavailable')
            b.run(['exec','-i',name,'pg_restore','-U','planner','-d','planner','--no-owner','--no-acl','--exit-on-error'], values['database.dump'])
            b.run(['cp',str(Path(self.record['source'])/'production/bin/planner'),name+':/tmp/planner'])
            b.run(['exec','--user','0:0',name,'chmod','755','/tmp/planner'])
            b.run(['exec',name,'psql','-U','planner','-d','planner','-v','ON_ERROR_STOP=1','-c','CREATE ROLE rehearsal_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;'])
            config=json.dumps({'database_url':'postgres://planner@127.0.0.1:5432/planner?sslmode=disable','runtime_role':'rehearsal_runtime'})
            b.run(['exec','-i',name,'sh','-c','umask 077; cat > /tmp/migration.json'],config.encode())
            b.run(['exec',name,'/tmp/planner','migrate','--config','/tmp/migration.json'])
            # Readiness through a least-privileged fixture role; no outbound network.
            args=types.SimpleNamespace(planner_bin=str(Path(self.record['source'])/'production/bin/planner'))
            b.runtime_smoke(args,name,values)
        finally:
            b.run(['rm','-f',name])

    def stop_writers(self):
        install, compose, env = self.context(True)
        install.run(compose+['stop','proxy','api','worker'],env,capture=True)
        if install.run(compose+['ps','--status','running','-q','api','worker','proxy'],env,capture=True):
            raise RuntimeError('writers_running')
        self.record.pop('stop_unconfirmed',None); self.save()

    def activate(self):
        install, compose, env = self.context()
        # Do not expose public traffic or start background writers before checks.
        install.check_https(compose,env)
        install.run(compose+['run','--rm','--no-deps','init-secrets'],env,capture=True)
        install.run(compose+['run','--rm','--no-deps','migrate'],env,capture=True)
        install.run(compose+['run','--rm','--no-deps','calendar'],env,capture=True)
        install.run(compose+['up','-d','--no-deps','api'],env,capture=True)
        actual=install.run(compose+['ps','-q','api'],env,capture=True)
        if install.run(install.docker()+['inspect','--format','{{.Image}}',actual],capture=True) != self.record['images']['app']:
            raise RuntimeError('wrong_image')
        info=install.run(install.docker()+['exec',actual,'/app/planner','release-info'],capture=True)
        if json.loads(info).get('git_commit') != self.record['commit']: raise RuntimeError('wrong_commit')
        import time
        for attempt in range(30):
            try:
                result=install.run(install.docker()+['run','--rm','--network','container:'+actual,'--read-only','--cap-drop','ALL','--security-opt','no-new-privileges:true','--user','1000:1000','--entrypoint','wget',self.record['images']['proxy'],'-qO-','-T','3','http://127.0.0.1:8080/readyz'],capture=True)
                if json.loads(result).get('ready') is True: break
            except (RuntimeError,ValueError): pass
            time.sleep(.25)
        else: raise RuntimeError('candidate_unavailable')
        install.run(compose+['up','-d','--no-deps','proxy'],env,capture=True)
        install.wait_web(self.state,compose,env)
        install.run(compose+['up','-d','--no-deps','worker'],env,capture=True)

    def apply(self):
        if self.manifest() != self.record['previous']: raise ValueError('installation_changed')
        self.assert_current()
        # Recheck immutable source contract immediately before entering maintenance.
        self.compatible(Path(self.record['previous_source']),Path(self.record['source']))
        install, compose, env = self.context(True)
        self.record['writers_stopped'] = True; self.phase('backup')
        install.run(compose+['stop','proxy','api','worker'],env,capture=True)
        if install.run(compose+['ps','--status','running','-q','api','worker','proxy'],env,capture=True):
            raise RuntimeError('writers_running')
        self.snapshot('backup')
        self.archive('backup')
        self.record['migration_started'] = True; self.phase('migrate')
        self.phase('verify')
        self.activate()
        deployment={**self.record['previous'],'commit':self.record['commit'],'images':self.record['images'],
                    'deployed_at':datetime.datetime.now(datetime.timezone.utc).isoformat()}
        atomic(self.state/'deployment.json',deployment)
        self.record['writers_stopped'] = False; self.record['state']='succeeded'; self.phase('complete')

    def recover(self):
        self.phase('restore')
        install, compose, env = self.context(True)
        self.stop_writers()
        if self.record.get('migration_started'):
            values, meta = self.archive('backup')
            db=self.record['database']; suffix=self.record['id'][:12]
            failed='planner_failed_'+suffix
            def sql(statement):
                return self.backup.run(['exec','-i',db,'psql','-U','planner','-d','postgres','-At','-v','ON_ERROR_STOP=1'],statement.encode()).decode().strip()
            current_oid=sql("SELECT oid FROM pg_database WHERE datname='planner';")
            if self.record.get('restore_oid') == current_oid:
                # Rename transaction committed before a service/process interruption.
                self.record['database_restored']=True; self.save()
            if not self.record.get('database_restored'):
                if sql("SELECT 1 FROM pg_database WHERE datname='"+failed+"';"):
                    raise ValueError('ambiguous_database_state')
                # Each retry gets a new database; retain any failed partial restore.
                restored='planner_restore_'+suffix+'_'+secrets.token_hex(4)
                sql('CREATE DATABASE '+restored+' OWNER planner_migrator;')
                self.backup.run(['exec','-i',db,'pg_restore','-U','planner','--role','planner_migrator','-d',restored,'--no-owner','--no-acl','--exit-on-error'],values['database.dump'])
                self.backup.run(['exec','-i',db,'psql','-U','planner','-d',restored,'-v','ON_ERROR_STOP=1'],
                                b'GRANT CONNECT ON DATABASE '+restored.encode()+b' TO planner_runtime; GRANT USAGE ON SCHEMA public TO planner_runtime; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO planner_runtime; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO planner_runtime;')
                if self.backup.counts(db,restored) != meta['counts']: raise ValueError('restore_counts_mismatch')
                self.record['restore_prepared']=restored; self.record['failed_database']=failed
                self.record['restore_oid']=sql("SELECT oid FROM pg_database WHERE datname='"+restored+"';")
                self.save()
                sql("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname IN ('planner','"+restored+"') AND pid<>pg_backend_pid();")
                # Both renames commit atomically; a crash cannot leave the name absent.
                sql('BEGIN; ALTER DATABASE planner RENAME TO '+failed+'; ALTER DATABASE '+restored+' RENAME TO planner; COMMIT;')
                self.record['database_restored']=True; self.save()
            for name,data in values.items():
                if name.startswith('secrets/'):
                    target=self.state/'config'/name.removeprefix('secrets/')
                    if target.parent != self.state/'config': raise ValueError('invalid_backup_member')
                    tmp=target.with_name('.restore-'+uuid.uuid4().hex)
                    fd=os.open(tmp,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
                    with os.fdopen(fd,'wb') as stream: stream.write(data)
                    os.replace(tmp,target)
        with contextlib.redirect_stdout(io.StringIO()): install.prepare(self.state,{'images':self.record['previous']['images']})
        atomic(self.state/'deployment.json',self.record['previous'])
        self.record['writers_stopped']=False; self.record['state']='succeeded'; self.phase('complete')
