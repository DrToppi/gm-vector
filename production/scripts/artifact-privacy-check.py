#!/usr/bin/env python3
"""Inspect candidate history, context, packages and every image layer privately."""
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tarfile
import zipfile
import gzip

from artifact_privacy import (ApplicationAssets, ContentStream, fresh_build_inventory,
                              packaged_distribution, preflight_image_archives,
                              walk_git_batch, walk_tar)
from image_gate import (ARCHIVE_LIMIT, LABELS, REVISION, GateError, file_hash,
                        layer_stream, open_regular, read_json, require, validate_directory)
from image_producer import PrivateArgumentParser, docker_command, private_file, run_bounded, write_json

GITLEAKS_VERSION = '8.30.1'
GITLEAKS_HASH = '88f91962aa2f93ac6ab281d553b9e125f5197bbbce38f9f2437f7299c32e5509'


def canonical_hash(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


class HashOnly:
    """Second app-layer pass computes the final overlay; no extraction/following."""
    def consume(self, name, metadata, body=None, size=0, depth=0):
        digest, read = hashlib.sha256(), 0
        if body:
            while chunk := body.read(min(1024 * 1024, size - read + 1)):
                read += len(chunk)
                require(read <= size, 'app asset size mismatch')
                digest.update(chunk)
        require(read == size, 'truncated app asset')
        return digest.hexdigest()


def produce(args):
    source, work = args.source.resolve(), args.output_dir.expanduser().resolve()
    checker_root = Path(__file__).resolve().parents[2]
    require(not work.is_relative_to(source) and not work.is_relative_to(checker_root),
            'privacy evidence must be outside source and checker checkout')
    require(REVISION.fullmatch(args.source_revision), 'full source revision required')
    work.mkdir(mode=0o700, exist_ok=False)
    env = {k: v for k, v in os.environ.items() if not k.startswith(('GITLEAKS_', 'TRIVY_'))}
    env['GIT_TERMINAL_PROMPT'] = '0'
    count = 0

    def run(command, cwd=source, capture=False, extra_env=None, stdin_path=None, artifact_output=None,
            file_limit=None):
        nonlocal count
        count += 1
        return run_bounded(command, work / f'command-{count:03d}.log', cwd, extra_env or env,
                           capture=capture, stdin_path=stdin_path, artifact_output=artifact_output,
                           child_file_limit=file_limit)

    require(run(['git', 'rev-parse', 'HEAD'], capture=True).decode().strip() == args.source_revision,
            'privacy source revision mismatch')
    require(not run(['git', 'status', '--porcelain', '--untracked-files=all'], capture=True), 'source is dirty')
    require(run(['git', 'rev-parse', '--is-shallow-repository'], capture=True).strip() == b'false',
            'shallow Git history cannot establish privacy')
    run(['git', 'fsck', '--full', '--no-dangling'])
    scanner = Path(args.gitleaks_bin or shutil.which('gitleaks') or '').resolve()
    require(scanner.is_file() and file_hash(scanner) == GITLEAKS_HASH, 'unverified Gitleaks executable')
    require(run([str(scanner), 'version'], capture=True).decode().strip() == GITLEAKS_VERSION,
            'wrong Gitleaks version')
    scan_directory = args.scan_directory.resolve()
    preflight_image_archives(scan_directory)
    validated = validate_directory(scan_directory)
    raw_manifest = read_json(scan_directory / 'manifest.json')
    require(validated['source_revision'] == args.source_revision, 'scanned source does not match history')

    config, ignored = work / 'default-rules.toml', work / 'empty.ignore'
    with private_file(config) as output:
        output.write(b'[extend]\nuseDefault = true\n')
    with private_file(ignored):
        pass

    def detect(key, payload=None, expect_finding=False, git=False):
        report = work / (key + '-detector.json')
        with private_file(report):
            pass
        command = [str(scanner), 'git' if git else 'stdin', '--config', str(config),
                   '--gitleaks-ignore-path', str(ignored), '--ignore-gitleaks-allow', '--redact=100',
                   '--no-banner', '--log-level', 'error', '--report-format', 'json',
                   '--report-path', str(report), '--max-target-megabytes', '0',
                   '--max-archive-depth', '0', '--max-decode-depth', '5', '--timeout', '170']
        if git:
            command += ['--log-opts=--all --full-history -m --no-ext-diff', str(source)]
        code = 0
        try:
            run(command, stdin_path=payload, file_limit=32 * 1024 * 1024)
        except subprocess.CalledProcessError as exc:
            code = exc.returncode
        findings = read_json(report)
        require(isinstance(findings, list), 'malformed privacy scanner report')
        require((code == 1 and findings) if expect_finding else (code == 0 and not findings),
                'privacy finding, scanner failure or ineffective detector control')
        return file_hash(report)

    roots = []

    def inspect(key, fill, expect=False):
        path = work / (key + '-content.bin')
        with private_file(path) as output:
            stream = ContentStream(output)
            fill(stream)
            value = stream.receipt()
        report_hash = detect(key, path, expect_finding=expect)
        return {'id': key, 'status': 'PASS', 'members': value['members'], 'body_bytes': value['body_bytes'],
                'inventory_sha256': value['metadata_sha256'], 'content_sha256': file_hash(path),
                'scanner_report_sha256': report_hash}

    # Include raw objects, because patch scanning alone can skip binary history.
    objects = run(['git', 'rev-list', '--objects', '--all'], capture=True).splitlines()
    ids = list(dict.fromkeys(row.split(b' ', 1)[0].decode('ascii') for row in objects))
    require(0 < len(ids) <= 10000 and all(REVISION.fullmatch(x) for x in ids), 'Git history object budget/identity failure')
    requests, batch = work / 'git-objects.request', work / 'git-objects.batch'
    with private_file(requests) as output:
        output.write(('\n'.join(ids) + '\n').encode())
    run(['git', 'cat-file', '--batch'], stdin_path=requests, artifact_output=batch)
    def history(stream):
        with open_regular(batch, ARCHIVE_LIMIT) as data:
            walk_git_batch(data, stream, ids)
    roots.append(inspect('P01', history))
    git_report = detect('P01-patch', git=True)
    roots[-1]['scanner_report_sha256'] = canonical_hash([roots[-1]['scanner_report_sha256'], git_report])

    tracked = run(['git', 'ls-files', '-z'], capture=True).split(b'\0')
    def tracked_files(stream):
        for raw_name in filter(None, tracked):
            name = raw_name.decode('utf8')
            path = source / name
            require(not path.is_symlink(), 'source symlink requires explicit review')
            with open_regular(path, ARCHIVE_LIMIT) as body:
                stream.consume(name, {'type': 'tracked-source'}, body, path.stat().st_size)
    roots.append(inspect('P02', tracked_files))

    go = os.environ.get('GMA_GO_BIN') or shutil.which('go')
    if not go:
        candidate = Path.home() / '.local/share/gma-toolchain/go/bin/go'
        go = str(candidate) if candidate.is_file() else None
    require(go, 'Go required for fresh build provenance')
    expected_assets = fresh_build_inventory(source, args.source_revision, work, run, go)
    # Docker itself applies .dockerignore. This scratch COPY exports exactly its
    # copy-visible context; selected build recipes are also covered in P02/P04.
    recipe, context = work / 'context.Dockerfile', work / 'context.tar'
    with private_file(recipe) as output:
        output.write(b'FROM scratch\nCOPY . /\n')
    with private_file(context):
        pass
    run(docker_command(args.docker_host) + ['build', '--network=none', '--no-cache', '-f', str(recipe),
        '--output', 'type=tar,dest=' + str(context), str(source / 'production')])
    context.chmod(0o600)
    def context_files(stream):
        with open_regular(context, ARCHIVE_LIMIT) as data:
            walk_tar(data, stream)
    roots.append(inspect('P03', context_files))
    package = work / 'source.tar'
    run(['git', 'archive', '--format=tar', args.source_revision], artifact_output=package)
    package_env = dict(env)
    if args.docker_host:
        package_env['DOCKER_HOST'] = args.docker_host
        package_env.pop('DOCKER_CONTEXT', None)
    distribution_tar, distribution_record = packaged_distribution(
        source, work, raw_manifest['images'], run, package_env)
    write_json(work / 'distribution.json', distribution_record)
    def package_files(stream):
        # Scan the actual archive once recursively. A directory scan followed by
        # another archive scan would double all image/layer bodies needlessly.
        for name, path in [('source.tar', package), ('distribution.tar', distribution_tar)]:
            with open_regular(path, ARCHIVE_LIMIT) as data:
                stream.consume(name, {'type': 'distribution-archive'}, data, path.stat().st_size)
    roots.append(inspect('P04', package_files))

    assets = ApplicationAssets()
    def image_files(stream):
        for label in LABELS:
            image = raw_manifest['images'][label]
            names = [row['member'] for row in image['archive_inventory']['layers']]
            with tarfile.open(scan_directory / (label + '-image.tar'), mode='r:') as outer:
                for member in outer:
                    if member.isfile():
                        with outer.extractfile(member) as body:
                            stream.consume(member.name, {'type': 'image-archive', 'image': label}, body, member.size)
                    else:
                        stream.consume(member.name, {'type': 'image-archive-directory', 'image': label})
                if label == 'app':
                    for name in names:
                        observe, finish = assets.layer()
                        with outer.extractfile(name) as raw:
                            decoded = layer_stream(raw)
                            try:
                                walk_tar(decoded, HashOnly(), observe)
                            finally:
                                if decoded is not raw:
                                    decoded.close()
                        finish()
        require(assets.files == expected_assets, 'image runtime files do not match fresh source/build context')
    roots.append(inspect('P05', image_files))

    controls = []
    for index in range(3):
        marker = ('api_key = "' + secrets.token_urlsafe(36) + '"\n').encode()
        def control(stream, index=index, marker=marker):
            if index < 2:
                value = (b'\x7fELF\0' if index == 1 else b'') + marker
                stream.consume('inert/control.bin', {'type': 'inert-marker'}, io.BytesIO(value), len(value))
            else:
                # A lower-layer marker remains scanned after the whiteout.
                overlay = ApplicationAssets()
                for layer, name, value in [('lower', 'app/temporary.txt', marker),
                                            ('later', 'app/.wh.temporary.txt', b'')]:
                    output = io.BytesIO()
                    with tarfile.open(fileobj=output, mode='w') as archive:
                        item = tarfile.TarInfo(name); item.size = len(value)
                        archive.addfile(item, io.BytesIO(value))
                    stream.consume('inert/' + layer + '-layer.tar', {'type': 'inert-marker'},
                                   io.BytesIO(output.getvalue()), len(output.getvalue()))
                    observe, finish = overlay.layer()
                    walk_tar(io.BytesIO(output.getvalue()), HashOnly(), observe)
                    finish()
                require(not overlay.files, 'lower-layer deletion control was ineffective')
        controls.append(inspect('P06-control-' + str(index), control, expect=True))
    roots.append({'id': 'P06', 'status': 'PASS', 'members': sum(x['members'] for x in controls),
                  'body_bytes': sum(x['body_bytes'] for x in controls),
                  **{key: canonical_hash([x[key] for x in controls])
                     for key in ('inventory_sha256', 'content_sha256', 'scanner_report_sha256')}})
    require(not run(['git', 'status', '--porcelain', '--untracked-files=all'], capture=True)
            and run(['git', 'rev-parse', 'HEAD'], capture=True).decode().strip() == args.source_revision,
            'source changed during privacy/provenance check')
    receipt = {'format': 'gm-vector-artifact-privacy-v1', 'status': 'PASS',
               'source_revision': args.source_revision, 'source_tree_clean': True,
               'scanner': {'gitleaks_version': GITLEAKS_VERSION, 'binary_sha256': GITLEAKS_HASH},
               'scan_manifest_sha256': file_hash(scan_directory / 'manifest.json'),
               'image_ids': {label: raw_manifest['images'][label]['image_id'] for label in LABELS},
               'roots': roots, 'build': {'binary_sha256': expected_assets['app/planner'],
                   'web_inventory_sha256': canonical_hash({k: v for k, v in expected_assets.items() if k.startswith('app/web/')}),
                   'context_archive_sha256': file_hash(context), 'source_archive_sha256': file_hash(package),
                   'image_assets_sha256': canonical_hash(assets.files)}}
    write_json(work / 'privacy.json', receipt)
    print(json.dumps({'status': 'PASS', 'source_revision': args.source_revision,
                      'roots': [x['id'] for x in roots], 'images': list(LABELS)}))


def main():
    parser = PrivateArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--source-revision', required=True)
    parser.add_argument('--scan-directory', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    parser.add_argument('--gitleaks-bin')
    parser.add_argument('--docker-host')
    try:
        produce(parser.parse_args())
        return 0
    except (GateError, OSError, ValueError, KeyError, TypeError, AttributeError, OverflowError,
            EOFError, tarfile.TarError, zipfile.BadZipFile, subprocess.SubprocessError):
        print('BLOCKED: incomplete privacy/provenance check; inspect private evidence.', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
