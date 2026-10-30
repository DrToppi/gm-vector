#!/usr/bin/env python3
"""Synthetic nginx HTTPS regression using the packaged proxy image, never live state."""
import argparse
import http.client
import importlib.util
import ipaddress
import json
import os
import ssl
import subprocess
import tempfile
import time
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('installer_tls', ROOT / 'deploy/install.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def run(args):
    return subprocess.run(args, capture_output=True, text=True, timeout=60)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default='gm-vector-proxy:local')
    args = parser.parse_args()
    docker = m.docker()
    with tempfile.TemporaryDirectory(prefix='gm-tls-test-') as temp:
        d = Path(temp)
        # Only throwaway fixture credentials are mounted; allow the image's
        # unprivileged UID to read them under both rootful and rootless Docker.
        d.chmod(0o755)
        def openssl(*argv):
            result = run(['openssl', *map(str, argv)])
            assert result.returncode == 0, 'synthetic certificate generation failed'
        openssl('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', d/'ca.key', '-out', d/'ca.pem', '-days', '1', '-subj', '/CN=Synthetic test CA', '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign')
        openssl('req', '-newkey', 'rsa:2048', '-nodes', '-keyout', d/'server.key', '-out', d/'server.csr', '-subj', '/CN=planner.example.test')
        (d/'ext').write_text('subjectAltName=DNS:planner.example.test\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n')
        bundle = d/'bundle'
        bundle.mkdir(mode=0o755)
        openssl('x509', '-req', '-in', d/'server.csr', '-CA', d/'ca.pem', '-CAkey', d/'ca.key', '-CAcreateserial', '-out', bundle/'fullchain.pem', '-days', '1', '-extfile', d/'ext')
        (bundle/'privkey.pem').write_bytes((d/'server.key').read_bytes())
        (bundle/'wrong.key').write_bytes((d/'ca.key').read_bytes())
        for f in bundle.iterdir():
            f.chmod(0o644)
        # Docker reserves both published ports atomically. Asking the kernel for
        # a free port and closing that socket before docker run is a TOCTOU race.
        # The configured canonical origin is independent of fixture transport.
        origin = 'https://planner.example.test'
        template = (ROOT/'tls/nginx.files.conf').read_text().replace('__HOST__', 'planner.example.test').replace('__ORIGIN__', origin).replace('api:8080', '127.0.0.1:18080')
        upstream = '''
    server {
        listen 127.0.0.1:18080;
        location /readyz { default_type application/json; return 200 '{"ready":true}'; }
        location /client { return 200 "$http_x_gma_client_ip|$http_x_forwarded_for|$http_host|$http_x_forwarded_proto"; }
    }
'''
        before, close = template.rsplit('}', 1)
        template = before + upstream + '}' + close
        config = d/'nginx.conf'
        config.write_text(template)
        config.chmod(0o644)
        bad = d/'bad.conf'
        bad.write_text(template.replace('/etc/nginx/certs/privkey.pem', '/etc/nginx/certs/wrong.key'))
        bad.chmod(0o644)
        def container_options(path):
            return ['--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges', '--tmpfs', '/tmp:rw,noexec,nosuid,size=16m,mode=1777', '--mount', 'type=bind,src='+str(bundle)+',dst=/etc/nginx/certs,readonly', '--mount', 'type=bind,src='+str(path)+',dst=/etc/nginx/nginx.conf,readonly']
        image_user = run(docker+['image', 'inspect', '--format', '{{.Config.User}}', args.image])
        assert image_user.returncode == 0 and image_user.stdout.strip().split(':')[0] not in ('', '0', 'root'), 'proxy image must declare a non-root user'
        valid = run(docker+['run', '--rm', '--network', 'none', *container_options(config), args.image, 'nginx', '-t'])
        assert valid.returncode == 0, 'valid nginx configuration rejected: '+valid.stderr
        invalid = run(docker+['run', '--rm', '--network', 'none', *container_options(bad), args.image, 'nginx', '-t'])
        assert invalid.returncode != 0 and ('mismatch' in invalid.stderr or 'key values' in invalid.stderr), 'mismatched certificate/key not rejected specifically'
        name = 'gm-vector-https-check-' + uuid.uuid4().hex[:12]
        try:
            started = run(docker+['run', '-d', '--name', name, *container_options(config), '-p', '127.0.0.1::9443', '-p', '127.0.0.1::9080', args.image])
            assert started.returncode == 0, 'synthetic nginx failed to start: '+started.stderr
            mapped = run(docker+['inspect', '--format', '{{json .NetworkSettings.Ports}}', name])
            assert mapped.returncode == 0, 'cannot inspect fixture port mappings'
            bindings = json.loads(mapped.stdout)
            def published_port(target):
                entries = bindings[str(target)+'/tcp']
                assert len(entries) == 1 and entries[0]['HostIp'] == '127.0.0.1', 'fixture must remain loopback-only'
                value = int(entries[0]['HostPort'])
                assert 1 <= value <= 65535
                return value
            https, http_port = published_port(9443), published_port(9080)
            assert https != http_port, 'fixture ports must be distinct'
            trust = ssl.create_default_context(cafile=str(d/'ca.pem'))
            for _ in range(100):
                conn = m.LocalTLSConnection('planner.example.test', https, '127.0.0.1', context=trust, timeout=1)
                try:
                    conn.request('GET', '/readyz')
                    response = conn.getresponse()
                    assert response.status == 200 and json.loads(response.read())['ready'] is True
                    break
                except OSError as exc:
                    last_error = repr(exc)
                    time.sleep(.1)
                finally:
                    conn.close()
            else:
                raise RuntimeError('synthetic HTTPS not ready: '+last_error+' '+run(docker+['logs', name]).stderr)
            conn = m.LocalTLSConnection('planner.example.test', https, '127.0.0.1', context=trust, timeout=2)
            try:
                conn.request('GET', '/client', headers={'X-GMA-Client-IP': 'spoofed', 'X-Forwarded-For': 'forged', 'X-Forwarded-Proto': 'http', 'Host': 'planner.example.test:'+str(https)})
                client_ip, forwarded, host, proto = conn.getresponse().read().decode().split('|')
                ipaddress.ip_address(client_ip)
                assert forwarded == client_ip and proto == 'https' and host == 'planner.example.test:'+str(https), 'proxy did not overwrite untrusted headers/preserve host'
            finally:
                conn.close()
            redirect = http.client.HTTPConnection('127.0.0.1', http_port, timeout=2)
            try:
                redirect.request('GET', '/test?q=one', headers={'Host': 'attacker.example.test'})
                response = redirect.getresponse()
                assert response.status in (301, 308) and response.getheader('Location') == origin+'/test?q=one', 'redirect must use configured origin, not attacker Host'
                response.read()
            finally:
                redirect.close()
            for hostname, context in [('wrong.example.test', trust), ('planner.example.test', ssl.create_default_context())]:
                conn = m.LocalTLSConnection(hostname, https, '127.0.0.1', context=context, timeout=2)
                try:
                    conn.request('GET', '/readyz')
                    raise AssertionError('invalid certificate trust/hostname accepted')
                except ssl.SSLError:
                    pass
                finally:
                    conn.close()
        finally:
            run(docker+['rm', '-f', name])
    print('PASS: non-root nginx HTTPS, matching cert/key, wrong-key rejection, CA/hostname verification, trusted proxy headers and fixed-origin redirect. No certificate issuance.')


if __name__ == '__main__':
    main()
