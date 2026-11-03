"""Pinned Trivy producer; no Docker socket or source directory enters Trivy."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import secrets
import resource
import selectors
import signal
import subprocess
import sys
import tarfile
import time

from image_gate import (ARCHIVE_LIMIT, DIGEST, LABELS, REVISION, SCANNER, VERSION, GateError,
                        archive_inventory, file_hash, open_regular, read_json, require,
                        validate_db, validate_directory)


class PrivateArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        self.exit(2, 'BLOCKED: invalid verification arguments; consult --help.\n')


def private_file(path):
    return os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'wb')


def write_json(path, value):
    with private_file(path) as output:
        output.write((json.dumps(value, sort_keys=True, indent=2) + '\n').encode())


def run_bounded(command, log_path, cwd, env, capture=False, timeout=180, artifact_output=None,
                artifact_limit=ARCHIVE_LIMIT, stdin_path=None, child_file_limit=None):
    """Stream bounded channels; never buffer a whole tool response in memory.

    The archive channel has its own budget. An RLIMIT_FSIZE on docker itself
    would also truncate docker-save data and cannot enforce separate log limits.
    """
    require(not (capture and artifact_output), 'conflicting command output modes')
    metadata = artifact_output or log_path.with_suffix('.stdout')
    log = private_file(log_path)
    output = None
    input_source = None
    process = None
    exited = False
    selector = selectors.DefaultSelector()
    try:
        if capture or artifact_output:
            output = private_file(metadata)
        if stdin_path:
            input_source = open_regular(stdin_path, ARCHIVE_LIMIT)
        def direct_file_limit():
            resource.setrlimit(resource.RLIMIT_FSIZE, (child_file_limit, child_file_limit))
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, stdin=input_source or subprocess.DEVNULL,
                                   start_new_session=True,
                                   preexec_fn=direct_file_limit if child_file_limit is not None else None)
        for stream, destination, limit in (
            (process.stdout, output or log, artifact_limit if artifact_output else 1024 * 1024),
            (process.stderr, log, 1024 * 1024),
        ):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, (destination, limit))
        sizes = {log: 0}
        if output:
            sizes[output] = 0
        deadline = time.monotonic() + timeout
        while selector.get_map():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired(command, timeout)
            for key, _ in selector.select(min(remaining, 0.1)):
                data = os.read(key.fileobj.fileno(), 65536)
                if not data:
                    selector.unregister(key.fileobj)
                    continue
                destination, limit = key.data
                require(sizes[destination] + len(data) <= limit, 'tool output budget exceeded')
                destination.write(data)
                sizes[destination] += len(data)
        code = process.wait(timeout=max(0.001, deadline - time.monotonic()))
        exited = True
        if code:
            raise subprocess.CalledProcessError(code, command)
    finally:
        if process:
            if not exited:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            process.wait(timeout=5)
            process.stdout.close()
            process.stderr.close()
        selector.close()
        if output:
            output.close()
        if input_source:
            input_source.close()
        log.close()
    return metadata.read_bytes() if capture else None


def docker_command(host):
    host = host or os.environ.get('GMA_DOCKER_HOST') or os.environ.get('DOCKER_HOST')
    if not host and Path(f'/run/user/{os.getuid()}/docker.sock').exists():
        host = f'unix:///run/user/{os.getuid()}/docker.sock'
    if host:
        require(host.startswith('unix:///') and '\n' not in host, 'remote Docker endpoint refused')
    return ['docker'] + (['--host', host] if host else [])


def produce(args):
    require(REVISION.fullmatch(args.source_revision), 'source revision must be a full Git SHA')
    directory = args.output_dir.expanduser().resolve()
    root = Path(__file__).resolve().parents[2]
    require(not directory.is_relative_to(root), 'raw image reports must remain outside the repository')
    directory.mkdir(mode=0o700, parents=False, exist_ok=False)
    docker = docker_command(args.docker_host)
    env = {key: value for key, value in os.environ.items() if not key.startswith(('TRIVY_', 'GITLEAKS_'))}
    started = dt.datetime.now(dt.timezone.utc).isoformat()
    counter = 0

    def run(command, capture=False, container_name=None, artifact_output=None):
        nonlocal counter
        counter += 1
        try:
            return run_bounded(command, directory / f'command-{counter:02d}.log', directory, env,
                               capture, artifact_output=artifact_output)
        except BaseException:
            if container_name:
                subprocess.run(docker + ['rm', '-f', container_name], cwd=directory, env=env,
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15, check=False)
            raise

    def scanner(arguments, mounts, network='none'):
        name = 'gm-vector-scan-' + secrets.token_hex(8)
        command = docker + ['run', '--rm', '--name', name, '--read-only', '--network', network,
                            '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
                            '--tmpfs', '/tmp:size=64m,mode=1777', '--memory', '1g', '--cpus', '2', '--pids-limit', '256',
                            '--ulimit', 'fsize=' + ('2147483648:2147483648' if network != 'none' else '33554432:33554432'),
                            '--workdir', '/tmp']
        for source, target, readonly in mounts:
            command += ['--mount', f'type=bind,src={source},dst={target}' + (',readonly' if readonly else '')]
        return run(command + [SCANNER, '--config', '', '--cache-dir', '/cache', *arguments],
                   container_name=name)

    images = {}
    for label in LABELS:
        reference = getattr(args, label + '_image')
        require(reference and not reference.startswith('-') and len(reference) <= 256, 'invalid image reference')
        info = json.loads(run(docker + ['image', 'inspect', reference], capture=True))
        require(isinstance(info, list) and len(info) == 1, 'missing candidate image')
        image_id = info[0].get('Id')
        require(isinstance(image_id, str) and DIGEST.fullmatch(image_id), 'invalid candidate image ID')
        images[label] = {'image_id': image_id, 'diff_ids': info[0].get('RootFS', {}).get('Layers'),
                         'registry_manifest_digests': info[0].get('RepoDigests', [])}
    def probe(image, arguments):
        name = 'gm-vector-probe-' + secrets.token_hex(8)
        return run(docker + ['run', '--rm', '--name', name, '--network', 'none', '--read-only',
                             '--memory', '256m', '--pids-limit', '64', '--cpus', '1',
                             '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
                             image, *arguments], capture=True, container_name=name)

    version = probe(SCANNER, ['--version'])
    require(version.decode().strip() == 'Version: ' + VERSION, 'unexpected pinned scanner version')
    release = json.loads(probe(images['app']['image_id'], ['release-info']))
    require(release.get('git_commit') == args.source_revision, 'candidate binary source revision mismatch')
    for label, image in images.items():
        archive = directory / (label + '-image.tar')
        run(docker + ['save', image['image_id']], artifact_output=archive)
        image['archive_inventory'] = archive_inventory(archive, image['image_id'], image['diff_ids'])
        image['archive_sha256'] = image['archive_inventory']['archive_sha256']

    cache = directory / 'cache'
    cache.mkdir(mode=0o700)
    scanner(['image', '--download-db-only', '--disable-telemetry', '--skip-version-check', '--no-progress', '--timeout', '170s'],
            [(cache, '/cache', False)], network='bridge')
    metadata_path = cache / 'db' / 'metadata.json'
    database = read_json(metadata_path)
    validate_db(database, dt.datetime.now(dt.timezone.utc))
    database_hash = file_hash(cache / 'db' / 'trivy.db')
    write_json(directory / 'db-metadata.json', database)

    for label, image in images.items():
        archive = directory / (label + '-image.tar')
        generated = directory / (label + '-generated')
        generated.mkdir(mode=0o700)
        report_path, sbom_path = generated / 'report.json', generated / 'sbom.json'
        for output in (report_path, sbom_path):
            with private_file(output):
                pass
        scanner(['image', '--input', '/input/image.tar', '--format', 'json',
                 '--output', '/output/report.json', '--scanners', 'vuln', '--pkg-types', 'os,library',
                 '--list-all-pkgs=true', '--severity', 'UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL',
                 '--ignorefile', '', '--ignore-unfixed=false', '--skip-db-update', '--skip-java-db-update',
                 '--offline-scan', '--disable-telemetry', '--skip-version-check', '--no-progress', '--timeout', '170s'],
                [(archive, '/input/image.tar', True), (cache, '/cache', False),
                 (cache / 'db', '/cache/db', True), (generated, '/output', False)])
        # Move JSON outside the writable conversion output before mounting it RO.
        final_report = directory / (label + '-vulns.json')
        report_path.rename(final_report)
        image['conversion_started_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
        scanner(['convert', '--format', 'cyclonedx', '--scanners', 'vuln', '--ignorefile', '',
                 '--severity', 'UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL', '--output', '/output/sbom.json',
                 '/input/report.json'],
                [(final_report, '/input/report.json', True), (generated, '/output', False),
                 (cache, '/cache', False)])
        image['conversion_finished_at'] = dt.datetime.now(dt.timezone.utc).isoformat()
        final_sbom = directory / (label + '-sbom.json')
        sbom_path.rename(final_sbom)
        require(file_hash(archive) == image['archive_sha256'], 'scanner modified its image input')
        image['report_sha256'], image['sbom_sha256'] = file_hash(final_report), file_hash(final_sbom)
        require(file_hash(cache / 'db' / 'trivy.db') == database_hash and read_json(metadata_path) == database,
                'advisory database changed during scan')

    receipt = {'format': 'gm-vector-image-scan-v1', 'source_revision': args.source_revision,
               'started_at': started, 'finished_at': dt.datetime.now(dt.timezone.utc).isoformat(),
               'scanner': {'image': SCANNER, 'version': VERSION}, 'candidate_release': release,
               'db_metadata_sha256': file_hash(directory / 'db-metadata.json'), 'db_sha256': database_hash,
               'db_updated_at': database['UpdatedAt'], 'approval_mode': 'disabled', 'exceptions': [], 'images': images}
    write_json(directory / 'manifest.json', receipt)
    summary = validate_directory(directory)
    write_json(directory / 'gate.json', summary)
    public = {label: {'packages': value['packages'], 'findings': len(value['findings']),
                      'blocking': len(value['blocking'])} for label, value in summary['images'].items()}
    print(json.dumps({'status': summary['status'], 'source_revision': args.source_revision,
                      'db_updated_at': summary['db_updated_at'], 'images': public}))
    return 0 if summary['status'] == 'PASS' else 1


def main():
    parser = PrivateArgumentParser(description=__doc__)
    for label in LABELS:
        parser.add_argument('--' + label + '-image', required=True)
    parser.add_argument('--source-revision', required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    parser.add_argument('--docker-host', help='optional local unix:/// Docker socket')
    try:
        return produce(parser.parse_args())
    except (GateError, ValueError, KeyError, TypeError, AttributeError, OSError, OverflowError,
            EOFError, tarfile.TarError, subprocess.SubprocessError):
        print('BLOCKED: incomplete or invalid image scan; inspect private evidence.', file=sys.stderr)
        return 2
