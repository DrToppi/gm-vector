#!/usr/bin/env python3
"""GM Vector local installation and HTTPS management."""
import argparse, base64, hashlib, http.client, ipaddress, json, os, re, secrets, socket, ssl, subprocess, sys, time, uuid
from urllib.parse import urlsplit
from pathlib import Path
ROOT = Path(__file__).resolve().parent


def private(path, value):
    data = value if isinstance(value, bytes) else json.dumps(value, ensure_ascii=False, indent=2).encode()
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(data); stream.flush(); os.fsync(stream.fileno())


def save_recovery_link(state, target, link):
    origin = json.loads((state/'config/config.json').read_text())['origin']
    parsed = urlsplit(origin)
    prefix = origin + '/#setup='
    if parsed.scheme != 'https' or parsed.username is not None or parsed.password is not None or parsed.path or parsed.query or parsed.fragment or not parsed.hostname:
        raise ValueError('invalid recovery origin')
    if not link.startswith(prefix) or not re.fullmatch(r'[A-Za-z0-9_-]{32,128}', link[len(prefix):]):
        raise ValueError('invalid recovery response')
    private(target/'recovery-url.txt', link.encode())


def docker():
    socket = Path('/run/user') / str(os.getuid()) / 'docker.sock'
    if os.environ.get('DOCKER_HOST') or os.environ.get('DOCKER_CONTEXT') or not socket.exists():
        return ['docker']
    return ['docker', '--host', 'unix://' + str(socket)]


def package():
    if not (ROOT/'package.json').exists():
        images={}
        for name,default in [('app','gm-vector:local'),('db','gm-vector-db:18.6'),('proxy','gm-vector-proxy:local')]:
            ref=os.environ.get('GM_VECTOR_'+name.upper()+'_IMAGE',default)
            images[name]=run(docker()+['image','inspect','--format','{{.Id}}',ref],capture=True)
        return {'format':'gm-vector-package-v1','images':images,'files':{}}
    manifest = json.loads((ROOT / 'package.json').read_text())
    if manifest.get('format') != 'gm-vector-package-v1':
        raise ValueError('invalid package')
    if set(manifest['images']) != {'app', 'db', 'proxy'}:
        raise ValueError('missing images')
    for image in manifest['images'].values():
        if not image.startswith('sha256:') or len(image) != 71:
            raise ValueError('invalid image identity')
    for name, digest in manifest['files'].items():
        path = ROOT / name
        if Path(name).is_absolute() or '..' in Path(name).parts or path.is_symlink():
            raise ValueError('invalid package path')
        h = hashlib.sha256()
        with path.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024*1024), b''): h.update(chunk)
        if h.hexdigest() != digest: raise ValueError('package checksum mismatch')
    return manifest



def report_public_key(path):
    """Read public-only support JWK; reject private or unexpected parameters."""
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 16384:
        raise ValueError('invalid support public key file')
    def unique(pairs):
        result = {}
        for name, value in pairs:
            if name in result: raise ValueError('duplicate public key field')
            result[name] = value
        return result
    key = json.loads(path.read_bytes(), object_pairs_hook=unique)
    if set(key) != {'kty','n','e','alg','use','kid'} or any(not isinstance(v,str) for v in key.values()):
        raise ValueError('public-only support key required')
    if (key['kty'],key['e'],key['alg'],key['use']) != ('RSA','AQAB','RSA-OAEP-256','enc'):
        raise ValueError('unsupported support key')
    modulus = base64.b64decode(key['n'], altchars=b'-_', validate=True)
    if len(modulus) != 384 or modulus[0] & 128 == 0 or modulus[-1] & 1 == 0 or base64.urlsafe_b64encode(modulus).decode().rstrip('=') != key['n']:
        raise ValueError('invalid support modulus')
    canonical = json.dumps({name:key[name] for name in ('e','kty','n')},separators=(',',':')).encode()
    if base64.urlsafe_b64encode(hashlib.sha256(canonical).digest()).decode().rstrip('=') != key['kid']:
        raise ValueError('invalid support key fingerprint')
    return key


def initialize(state, port, subnet):
    import ipaddress
    network = ipaddress.ip_network(subnet)
    if network.version != 4 or network.prefixlen != 29 or not network.is_private:
        raise ValueError('use a private IPv4 /29 subnet')
    if not 1024 <= port <= 65535: raise ValueError('port must be 1024..65535')
    support_path = ROOT / 'report-public-key.json'
    support_key = report_public_key(support_path) if support_path.exists() or support_path.is_symlink() else None
    state.mkdir(mode=0o700, parents=True, exist_ok=False)
    cfg = state / 'config'; cfg.mkdir(mode=0o700)
    instance = uuid.uuid4().hex
    bootstrap, runtime, migration = [secrets.token_urlsafe(36) for _ in range(3)]
    for name, value in {'database.password': bootstrap.encode(), 'bootstrap.token': secrets.token_urlsafe(36).encode(), 'encryption.key': secrets.token_bytes(32), 'instance.id': instance.encode()}.items():
        private(cfg/name, value)
    if support_key is not None: private(cfg/'report-public-key.json', support_key)
    private(cfg/'config.json', {'report_directory': '/var/lib/planner-reports', **({'report_public_key_file': '/run/secrets/report-public-key.json'} if support_key is not None else {}), 'database_url': f'postgres://planner_runtime:{runtime}@db:5432/planner?sslmode=disable', 'origin': f'https://localhost:{port}', 'web_dir': '/app/web', 'secret_file': '/run/secrets/encryption.key', 'bootstrap_file': '/run/secrets/bootstrap.token'})
    private(cfg/'migration-config.json', {'database_url': f'postgres://planner_migrator:{migration}@db:5432/planner?sslmode=disable', 'runtime_role': 'planner_runtime'})
    private(state/'installation.json', {'project': 'gm-vector-'+instance[:12], 'port': port, 'subnet': str(network), 'proxy_ip': str(network.network_address+2), 'api_ip': str(network.network_address+3)})
    private(state/'Caddyfile', b"{\n admin off\n auto_https disable_redirects\n}\nhttps://localhost:9443 {\n tls internal\n encode zstd gzip\n reverse_proxy api:8080 {\n  header_up X-GMA-Client-IP {http.request.remote.host}\n }\n}\n")
    os.chmod(state/'Caddyfile', 0o644)  # Public proxy config; parent remains private.
    setup='https://localhost:'+str(port)+'/#setup='+(cfg/'bootstrap.token').read_text().strip()
    private(state/'setup-url.txt',setup.encode())
    print('Installation initialized. First-run URL is stored privately in '+str(state/'setup-url.txt'))


def https_options(state):
    """Optional operator configuration; no secrets, arbitrary targets or TLS bypass."""
    path = state/'https.json'
    if not path.exists(): return None
    options = json.loads(path.read_text())
    allowed = {'mode','origin','bind_address','http_port','cert_dir','ca_file'}
    if not isinstance(options,dict) or set(options)-allowed:
        raise ValueError('unknown HTTPS setting')
    if options.get('mode') not in ('files','acme'):
        raise ValueError('HTTPS mode must be files or acme')
    origin = options.get('origin','')
    if not isinstance(origin,str) or any(c.isspace() for c in origin):
        raise ValueError('invalid HTTPS origin')
    parsed = urlsplit(origin)
    host = parsed.hostname
    if parsed.scheme!='https' or not host or parsed.username or parsed.password or parsed.path or parsed.query or parsed.fragment:
        raise ValueError('HTTPS origin must contain only scheme, host and optional port')
    try: ipaddress.ip_address(host)
    except ValueError:
        if len(host)>253 or not all(re.fullmatch(r'[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?',label) for label in host.split('.')):
            raise ValueError('invalid HTTPS hostname; use ASCII/punycode')
    port = parsed.port or 443
    if not 1<=port<=65535: raise ValueError('invalid HTTPS port')
    bind = options.get('bind_address','127.0.0.1')
    ipaddress.ip_address(bind)
    http_port = options.get('http_port',80)
    if type(http_port) is not int or not 1<=http_port<=65535 or http_port==port:
        raise ValueError('invalid HTTP port')
    if options['mode']=='acme' and (port!=443 or http_port!=80):
        raise ValueError('ACME requires external ports 80 and 443 in this profile')
    runtime = json.loads((state/'config/config.json').read_text())
    if runtime.get('origin')!=origin:
        raise ValueError('HTTPS origin must exactly match backend configuration')
    if options['mode']=='files':
        directory=Path(options.get('cert_dir',''))
        if not directory.is_absolute() or not directory.is_dir():
            raise ValueError('absolute certificate directory required')
        for name in ('fullchain.pem','privkey.pem'):
            item=directory/name
            if item.is_symlink() or not item.is_file(): raise ValueError('regular PEM files required')
        if (directory/'privkey.pem').stat().st_mode & 0o007:
            raise ValueError('private key must not be world-accessible')
    elif 'cert_dir' in options: raise ValueError('cert_dir only applies to files mode')
    if options.get('ca_file'):
        ca=Path(options['ca_file'])
        if not ca.is_absolute() or not ca.is_file(): raise ValueError('absolute CA file required')
    return {**options,'hostname':host,'port':port,'http_port':http_port,'bind_address':bind}


def context(state, manifest):

    info = json.loads((state/'installation.json').read_text())
    env = {**os.environ, 'GMA_PROJECT': info['project'], 'GMA_SECRETS_DIR': str(state/'config'), 'GMA_CADDYFILE': str(state/'Caddyfile'), 'GMA_HTTPS_PORT': str(info['port']), 'GMA_EDGE_SUBNET': info['subnet'], 'GMA_PROXY_IP': info['proxy_ip'], 'GMA_API_IP': info['api_ip']}
    for key, value in manifest['images'].items(): env['GMA_'+key.upper()+'_IMAGE'] = value
    # Never silently accept an inherited synthetic runtime opt-in for customer images.
    env.pop('GMA_ALLOW_SYNTHETIC_BUILD', None)
    command=docker()+['compose', '-f', str(ROOT/'compose.yaml')]
    options=https_options(state)
    if options:
        profiles=ROOT if (ROOT/'compose.https.yaml').exists() else ROOT.parent
        command+=['-f',str(profiles/'compose.https.yaml')]
        env.update(GMA_PUBLIC_ORIGIN=options['origin'],GMA_HTTPS_HOST=('['+options['hostname']+']' if ':' in options['hostname'] else options['hostname']),GMA_HTTPS_PORT=str(options['port']),GMA_HTTP_PORT=str(options['http_port']),GMA_BIND_ADDRESS=('['+options['bind_address']+']' if ':' in options['bind_address'] else options['bind_address']))
        if options['mode']=='files':
            command+=['-f',str(profiles/'compose.https.files.yaml')]
            env['GMA_TLS_DIR']=options['cert_dir']
    return command, env


def run(command, env=None, capture=False):
    result = subprocess.run(command, env=env, stdout=subprocess.PIPE if capture else None, stderr=subprocess.PIPE if capture else None, timeout=300)
    if result.returncode: raise RuntimeError('Docker operation failed')
    return result.stdout.decode().strip() if capture else ''


class LocalTLSConnection(http.client.HTTPSConnection):
    """Connect to this host, but verify the public hostname and send its Host."""
    def __init__(self, hostname, port, connect_host, **kwargs):
        super().__init__(hostname,port,**kwargs)
        self.connect_host=connect_host
    def connect(self):
        raw=socket.create_connection((self.connect_host,self.port),self.timeout)
        try: self.sock=self._context.wrap_socket(raw,server_hostname=self.host)
        except BaseException:
            raw.close(); raise


def check_https(compose, env):
    # Validation failure aborts before touching a running proxy. No port publish.
    run(compose+['run','--rm','--no-deps','proxy','caddy','validate','--config','/etc/caddy/Caddyfile','--adapter','caddyfile'],env,capture=True)


def wait_web(state, compose, env, timeout=120):
    """Verify local HTTPS with the CA read from this installation's proxy only."""
    port = json.loads((state/'installation.json').read_text())['port']
    options=https_options(state)
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        connection = None
        try:
            if options:
                tls=ssl.create_default_context(cafile=options.get('ca_file'))
                bind=options['bind_address']
                connect_host='127.0.0.1' if bind=='0.0.0.0' else '::1' if bind=='::' else bind
                connection=LocalTLSConnection(options['hostname'],options['port'],connect_host,context=tls,timeout=5)
                connection.request('GET','/readyz',headers={'Host':urlsplit(options['origin']).netloc})
            else:
                container = run(compose+['ps','-q','proxy'],env,capture=True)
                if not container or '\n' in container:
                    raise RuntimeError('proxy is not ready')
                pem = run(docker()+['exec',container,'cat','/data/caddy/pki/authorities/local/root.crt'],capture=True)
                tls = ssl.create_default_context(cadata=pem)
                # Direct connection: never use HTTP proxy environment or follow redirects.
                connection = http.client.HTTPSConnection('localhost',port,context=tls,timeout=5)
                connection.request('GET','/readyz')
            response = connection.getresponse()
            if response.status == 200 and json.loads(response.read(4096)).get('ready') is True:
                return
        except (OSError, ValueError, RuntimeError, http.client.HTTPException):
            pass  # Startup retries intentionally omit diagnostics that could expose secrets.
        finally:
            if connection is not None:
                connection.close()
        time.sleep(1)
    raise RuntimeError('local HTTPS readiness timed out; installation state was preserved')


def prepare(state, manifest):
    """Start the workspace without creating an administrator automatically."""
    # Read state first: missing/partial initialization must not start Docker services.
    compose, env = context(state, manifest)
    for name in ('config.json','migration-config.json','database.password','bootstrap.token','encryption.key'):
        if not (state/'config'/name).is_file():
            raise ValueError('incomplete installation state')
    if (ROOT/'images.tar').is_file(): run(docker()+['load','--input',str(ROOT/'images.tar')],capture=True)
    for image in manifest['images'].values():
        if run(docker()+['image','inspect','--format','{{.Id}}',image],capture=True) != image:
            raise ValueError('loaded image mismatch')
    run(compose+['run','--rm','--no-deps','init-secrets'],env)
    run(compose+['up','-d','--no-deps','--wait','db'],env)
    container = run(compose+['ps','-q','db'],env,capture=True)
    if not container or '\n' in container:
        raise RuntimeError('database container is unavailable')
    run([sys.executable,str(ROOT/'database-roles.py' if (ROOT/'database-roles.py').exists() else ROOT.parent/'scripts/database-roles.py'),'--config',str(state/'config'),'--container',container,'--apply'])
    run(compose+['run','--rm','--no-deps','init-secrets'],env)
    run(compose+['run','--rm','--no-deps','migrate'],env)
    run(compose+['run','--rm','--no-deps','calendar'],env)
    if https_options(state): check_https(compose,env)
    run(compose+['up','-d','--no-deps','api','worker','proxy'],env)
    wait_web(state,compose,env)
    origin=env.get('GMA_PUBLIC_ORIGIN','https://localhost:'+env['GMA_HTTPS_PORT'])
    print('Workspace: '+origin)
    setup=origin+'/#setup='+(state/'config/bootstrap.token').read_text().strip()
    target=state/'setup-url.txt'
    pending=state/('setup-url.'+uuid.uuid4().hex+'.tmp')
    private(pending,setup.encode());os.replace(pending,target)
    print('First administrator: open the private URL in '+str(target))
    print('Create your administrator credentials and confirm TOTP. Keep recovery codes offline.')


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('command',choices=['init','prepare','start','stop','status','recover-admin','check-https','apply-https'])
    p.add_argument('--state',type=Path,required=True,help='new private local data directory')
    p.add_argument('--username',help='administrator username for recover-admin')
    p.add_argument('--port',type=int,default=9443)
    p.add_argument('--subnet',default='172.30.90.0/29')
    a=p.parse_args(); state=a.state.expanduser().resolve(); manifest=package()
    if a.command=='init': initialize(state,a.port,a.subnet); return
    if a.command=='prepare': prepare(state,manifest); return
    compose,env=context(state,manifest)
    if a.command=='recover-admin':
        if not a.username: p.error('--username is required')
        container=run(compose+['ps','-q','api'],env,capture=True)
        if not container or '\n' in container: raise RuntimeError('API is not running')
        target=state/('recovery-'+uuid.uuid4().hex)
        target.mkdir(mode=0o700)
        remote='/tmp/'+target.name+'.txt'
        link=run(docker()+['exec',container,'/app/planner','recover-user','--username',a.username,'--file',remote,'--output-stdout'],capture=True)
        save_recovery_link(state,target,link)
        print('Recovery link: '+str(target/'recovery-url.txt')+' (15 minutes, single use).')
        print('Previous sessions and API tokens have been revoked. Delete the local file after use.')
        return
    if a.command=='check-https': check_https(compose,env); return
    if a.command=='apply-https':
        check_https(compose,env)
        run(compose+['run','--rm','--no-deps','init-secrets'],env)
        run(compose+['up','-d','--no-deps','--force-recreate','api','worker','proxy'],env)
        wait_web(state,compose,env)
        print('HTTPS configuration applied and verified.')
        return
    if a.command=='start' and https_options(state): check_https(compose,env)
    commands={'start':['up','-d','--no-deps','db','api','worker','proxy'],'stop':['stop'],'status':['ps']}
    run(compose+commands[a.command],env)

if __name__=='__main__':
    try: main()
    except (Exception,KeyboardInterrupt):
        print('Operation failed. Check the package, path, Docker and configuration. Secrets are not printed. Keep the installation directory for recovery.',file=sys.stderr);sys.exit(1)
