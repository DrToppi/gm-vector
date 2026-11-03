"""Bounded archive inspection without extraction or following archive links."""
import hashlib
from contextlib import contextmanager
import bz2
import gzip
import json
import lzma
import os
from pathlib import Path
from pathlib import PurePosixPath
import shlex
import shutil
import sys
import struct
import io
import re
import tarfile
import tempfile
import zipfile

from image_gate import ARCHIVE_LIMIT, LAYER_LIMIT, GateError, file_hash, json_bytes, open_regular, read_json, require, safe_member

MAX_MEMBERS = 100000
MAX_EXTENSION = 64 * 1024
MAX_EXTENSIONS = 1024 * 1024
MAX_METADATA = 64 * 1024
NOTICE_FILES = frozenset(('THIRD-PARTY-NOTICES.txt', 'inventory.json'))
FORBIDDEN_NAMES = frozenset(('encryption.key', 'database.password', 'instance.id',
                             'bootstrap.token', 'updater.token', 'signing.key',
                             'support-private.pem', 'credentials.json'))


class ContentStream:
    """Prefix binary bodies so the detector does not skip their MIME type.

    Every byte is streamed, including binary bodies. This is pattern detection,
    not a claim that opaque/encrypted data can be recognized as a secret.
    """
    def __init__(self, output):
        self.output = output
        self.members = 0
        self.body_bytes = 0
        self.written_bytes = 0
        self.metadata_sha = hashlib.sha256()

    def write(self, value):
        require(self.written_bytes + len(value) <= ARCHIVE_LIMIT, 'serialized artifact budget exceeded')
        self.output.write(value)
        self.written_bytes += len(value)

    def framing(self, raw):
        # Include otherwise ignored header fields and file padding. The byte
        # budget also covers these prefixes; no raw framing is parsed as a new
        # nested archive and no header/body byte can hide past tarfile's EOF.
        self.metadata_sha.update(b'TAR-FRAMING\0' + raw)
        self.write(b'\nGM_VECTOR_TAR_FRAMING\n' + raw + b'\n')

    def consume(self, name, metadata, body=None, size=0, depth=0):
        safe_member(name)
        require(self.members < MAX_MEMBERS, 'artifact member budget exceeded')
        require(PurePosixPath(name).name not in FORBIDDEN_NAMES, 'private installation artifact in distribution')
        require(0 <= size <= LAYER_LIMIT, 'artifact member too large')
        self.members += 1
        header = json.dumps({'member': self.members, 'path': name, **metadata}, sort_keys=True).encode()
        require(len(header) <= MAX_METADATA, 'artifact metadata budget exceeded')
        self.metadata_sha.update(header + b'\n')
        self.write(b'\nGM_VECTOR_INSPECTED_MEMBER\n' + header + b'\n')
        digest, read, nested, kind = hashlib.sha256(), 0, None, None
        try:
            if body is not None:
                while True:
                    chunk = body.read(min(1024 * 1024, size - read + 1))
                    if not chunk:
                        break
                    if read == 0:
                        kind = archive_kind(chunk, name)
                        if kind:
                            require(depth < 5, 'nested archive depth exceeded')
                            nested = tempfile.SpooledTemporaryFile(max_size=1024 * 1024, mode='w+b')
                    read += len(chunk)
                    self.body_bytes += len(chunk)
                    require(read <= size and self.body_bytes <= ARCHIVE_LIMIT, 'artifact content budget exceeded')
                    digest.update(chunk)
                    self.write(chunk)
                    if nested:
                        nested.write(chunk)
                require(read == size, 'truncated artifact member')
                if read == 0 and name.casefold().endswith('.zip'):
                    archive_kind(b'', name)
                if nested:
                    nested.seek(0)
                    inspect_nested(nested, kind, name, self, depth + 1)
            else:
                require(size == 0, 'artifact body missing')
        finally:
            if nested:
                nested.close()
        self.write(b'\n')
        return digest.hexdigest()

    def receipt(self):
        require(self.members > 0, 'empty artifact inventory')
        return {'members': self.members, 'body_bytes': self.body_bytes,
                'metadata_sha256': self.metadata_sha.hexdigest()}


def archive_kind(prefix, name):
    zip_layout = prefix.startswith((b'PK\x03\x04', b'PK\x05\x06'))
    require(not name.casefold().endswith('.zip') or zip_layout,
            'prefixed or unsupported named ZIP layout')
    require(not prefix.startswith((b'GMABK01', b'PGDMP', b'SQLite format 3\x00')),
            'private database or backup artifact in distribution')
    if prefix.startswith(b'\x1f\x8b'):
        return 'gzip'
    if zip_layout:
        return 'zip'
    if prefix.startswith(b'BZh'):
        return 'bzip2'
    if prefix.startswith(b'\xfd7zXZ\x00'):
        return 'xz'
    require(not prefix.startswith((b'\x28\xb5\x2f\xfd', b'7z\xbc\xaf\x27\x1c', b'Rar!')),
            'unsupported compressed artifact requires explicit inspection')
    if prefix[257:262] == b'ustar' or name.endswith('.tar'):
        return 'tar'
    return None


def checked_zip(source):
    """Cap the central directory before ZipFile reads/allocates all entries."""
    source.seek(0, os.SEEK_END)
    size = source.tell()
    require(size <= LAYER_LIMIT, 'ZIP size budget exceeded')
    source.seek(max(0, size - 65557))
    tail = source.read(65557)
    offset = tail.rfind(b'PK\x05\x06')
    require(offset >= 0 and len(tail) - offset >= 22, 'missing ZIP directory')
    fields = struct.unpack('<4s4H2IH', tail[offset:offset + 22])
    _, disk, directory_disk, disk_count, count, directory_size, directory_offset, comment = fields
    position = size - len(tail) + offset
    require(disk == directory_disk == 0 and disk_count == count and count <= MAX_MEMBERS
            and count != 65535 and directory_size <= 32 * 1024 * 1024
            and directory_offset + directory_size == position
            and position + 22 + comment == size, 'unsupported or oversized ZIP directory')
    source.seek(directory_offset)
    observed = consumed = 0
    while consumed < directory_size:
        header = source.read(46)
        require(len(header) == 46 and header[:4] == b'PK\x01\x02', 'malformed ZIP directory member')
        row = struct.unpack('<4s6H3I5H2I', header)
        name, extra, note = row[10:13]
        observed += 1
        consumed += 46 + name + extra + note
        require(observed <= count and name <= 1024 and extra + note <= MAX_EXTENSION
                and consumed <= directory_size, 'ZIP directory member budget exceeded')
        require(len(source.read(name + extra + note)) == name + extra + note,
                'truncated ZIP directory member')
    require(observed == count and consumed == directory_size, 'ZIP directory count mismatch')
    source.seek(0)


def inspect_nested(source, kind, name, content, depth):
    try:
        if kind == 'tar':
            walk_tar(source, content, depth=depth)
        elif kind == 'zip':
            checked_zip(source)
            with zipfile.ZipFile(source) as archive:
                require(len(archive.infolist()) <= MAX_MEMBERS, 'ZIP member budget exceeded')
                for member in archive.infolist():
                    safe_member(member.filename)
                    require(not member.flag_bits & 1, 'encrypted nested archive cannot be inspected')
                    with archive.open(member) as body:
                        content.consume(member.filename, {'type': 'zip-member'}, body, member.file_size, depth)
        else:
            opener = {'gzip': gzip.GzipFile, 'bzip2': bz2.BZ2File, 'xz': lzma.LZMAFile}[kind]
            decoder = opener(fileobj=source, mode='rb') if kind == 'gzip' else opener(source, mode='rb')
            with decoder:
                with tempfile.SpooledTemporaryFile(max_size=1024 * 1024, mode='w+b') as expanded:
                    size = 0
                    for chunk in iter(lambda: decoder.read(1024 * 1024), b''):
                        size += len(chunk)
                        require(size <= LAYER_LIMIT and content.body_bytes + size <= ARCHIVE_LIMIT,
                                'nested archive expansion budget exceeded')
                        expanded.write(chunk)
                    expanded.seek(0)
                    content.consume(name + '!decoded', {'type': 'decompressed'}, expanded, size, depth)
    except (OSError, EOFError, zipfile.BadZipFile, lzma.LZMAError, NotImplementedError, RuntimeError) as exc:
        raise GateError('malformed or unsupported nested archive') from exc


@contextmanager
def validated_tar(source, framing=None):
    """Bound framing/extension data before tarfile can allocate PAX/GNU bodies.

    tarfile intentionally stops at the first zero header and ignores remaining
    bytes. This pass checks every raw block, including all padding/trailers.
    Nothing is extracted and the private spool is closed on every failure.
    """
    with tempfile.SpooledTemporaryFile(max_size=1024 * 1024, mode='w+b') as spool:
        total = headers = extensions = chain = 0
        def read_exact(size):
            nonlocal total
            value = source.read(size)
            require(len(value) == size, 'truncated archive framing')
            total += len(value)
            require(total <= ARCHIVE_LIMIT, 'raw archive budget exceeded')
            spool.write(value)
            return value
        while True:
            block = read_exact(512)
            if not any(block):
                require(not any(read_exact(512)), 'malformed archive end marker')
                while True:
                    tail = source.read(512)
                    if not tail:
                        break
                    total += len(tail)
                    require(total <= ARCHIVE_LIMIT and len(tail) == 512 and not any(tail),
                            'nonzero or unparsed archive trailer')
                    spool.write(tail)
                break
            headers += 1
            require(headers <= MAX_MEMBERS, 'archive header budget exceeded')
            if framing:
                framing(block)
            info = tarfile.TarInfo.frombuf(block, 'utf-8', 'surrogateescape')
            require(0 <= info.size <= LAYER_LIMIT and info.type != tarfile.GNUTYPE_SPARSE,
                    'unsupported or oversized archive member')
            extension = info.type in (tarfile.XHDTYPE, tarfile.XGLTYPE, tarfile.GNUTYPE_LONGNAME,
                                      tarfile.GNUTYPE_LONGLINK)
            chain = chain + 1 if extension else 0
            require(chain <= 8, 'archive extension chain budget exceeded')
            if extension:
                extensions += info.size
                require(info.size <= MAX_EXTENSION and extensions <= MAX_EXTENSIONS,
                        'archive extension budget exceeded')
            remaining = info.size
            while remaining:
                part = read_exact(min(remaining, MAX_EXTENSION if extension else 1024 * 1024))
                if extension:
                    require(b'GNU.sparse' not in part and b'SCHILY.realsize' not in part,
                            'sparse archive metadata is unsupported')
                    if framing:
                        framing(part)
                remaining -= len(part)
            padding = (-info.size) % 512
            if padding:
                part = read_exact(padding)
                if framing:
                    framing(part)
        require(headers > 0, 'empty archive')
        spool.seek(0)
        yield spool


def walk_tar(source, content, on_member=None, depth=0):
    """Visit duplicate names too; links/special nodes outside /app are metadata."""
    count = 0
    try:
        with validated_tar(source, getattr(content, 'framing', None)) as checked, tarfile.open(fileobj=checked, mode='r|') as archive:
            for member in archive:
                count += 1
                require(count <= MAX_MEMBERS, 'archive member budget exceeded')
                safe_member(member.name)
                metadata = {'type': member.type.decode('ascii', errors='replace'),
                            'link': member.linkname, 'mode': member.mode,
                            'pax': member.pax_headers}
                if member.isfile():
                    with archive.extractfile(member) as body:
                        value = content.consume(member.name, metadata, body, member.size, depth)
                else:
                    require(member.size == 0, 'non-file archive member has a body')
                    value = content.consume(member.name, metadata)
                if on_member:
                    on_member(member, value)
    except (tarfile.TarError, EOFError, OSError, UnicodeError) as exc:
        raise GateError('malformed archive content') from exc
    require(count > 0, 'empty archive')
    return count


class LimitedObject:
    def __init__(self, source, kind, size):
        self.source, self.remaining = source, size
        self.hash = hashlib.sha1((kind + ' ' + str(size) + '\0').encode())

    def read(self, size):
        value = self.source.read(min(size, self.remaining))
        self.remaining -= len(value)
        self.hash.update(value)
        return value


def tree_entries(raw):
    offset, count = 0, 0
    while offset < len(raw):
        space = raw.find(b' ', offset)
        end = raw.find(b'\0', space + 1)
        require(offset < space < end and end + 21 <= len(raw), 'malformed Git tree')
        require(raw[offset:space] in (b'100644', b'100755', b'120000', b'40000'), 'unsupported Git tree mode')
        try:
            name = raw[space + 1:end].decode('utf8')
        except UnicodeError as exc:
            raise GateError('unsupported Git filename encoding') from exc
        safe_member(name)
        require('/' not in name and name not in FORBIDDEN_NAMES, 'private or unsafe filename in Git history')
        offset = end + 21
        count += 1
    return count


def walk_git_batch(source, content, expected_ids):
    """Inspect every reachable object, including deleted binary blobs/messages.

    Git's textual patch scanner alone does not expose binary blob contents.
    Fixed --batch framing is authenticated against each object's Git SHA-1.
    """
    count = 0
    for expected in expected_ids:
        header = source.readline(201)
        require(len(header) <= 200 and header.endswith(b'\n'), 'malformed Git batch header')
        fields = header.rstrip(b'\n').split(b' ')
        require(len(fields) == 3 and re.fullmatch(b'[0-9a-f]{40}', fields[0]), 'malformed Git object identity')
        require(fields[0].decode() == expected and fields[1] in (b'blob', b'tree', b'commit', b'tag')
                and re.fullmatch(b'[0-9]{1,10}', fields[2]), 'Git batch membership mismatch')
        kind, size = fields[1].decode(), int(fields[2])
        require(size <= LAYER_LIMIT, 'Git object size budget exceeded')
        body = LimitedObject(source, kind, size)
        metadata = {'git_type': kind, 'git_object': expected}
        if kind == 'tree':
            require(size <= 32 * 1024 * 1024, 'Git tree size budget exceeded')
            raw = body.read(size)
            metadata['tree_entries'] = tree_entries(raw)
            content.consume('history/' + expected, metadata, io.BytesIO(raw), size)
        else:
            content.consume('history/' + expected, metadata, body, size)
        require(body.remaining == 0 and body.hash.hexdigest() == expected, 'Git object content hash mismatch')
        require(source.read(1) == b'\n', 'truncated Git batch framing')
        count += 1
    require(count > 0 and not source.read(1), 'omitted or extra Git history objects')
    return count


class ApplicationAssets:
    """Apply whiteouts to hash inventory only; all versions remain inspected."""
    def __init__(self):
        self.files = {}

    def layer(self):
        additions, deletions, opaque = {}, [], []

        def observe(member, digest):
            path = str(PurePosixPath(member.name))
            if path != 'app' and not path.startswith('app/'):
                return
            require(member.isfile() or member.isdir(), 'link or special runtime asset refused')
            name = PurePosixPath(path).name
            parent = str(PurePosixPath(path).parent)
            if name == '.wh..wh..opq':
                opaque.append(parent + '/')
            elif name.startswith('.wh.'):
                deletions.append(parent + '/' + name[4:])
            elif member.isfile():
                additions[path] = digest
            elif not member.isdir():
                deletions.append(path)
                additions.pop(path, None)

        def finish():
            for path in list(self.files):
                if any(path == target or path.startswith(target + '/') for target in deletions) \
                   or any(path.startswith(prefix) for prefix in opaque):
                    del self.files[path]
            self.files.update(additions)
            for path in self.files:
                require(not any(str(parent) in self.files for parent in PurePosixPath(path).parents),
                        'non-directory runtime asset parent')

        return observe, finish


def bounded_paths(directory):
    """Bound a tree before sorting, without following any filesystem link."""
    pending, found = [Path(directory)], []
    while pending:
        with os.scandir(pending.pop()) as entries:
            for entry in entries:
                require(len(found) < MAX_MEMBERS and not entry.is_symlink(), 'artifact tree budget exceeded')
                path = Path(entry.path)
                require(entry.is_file(follow_symlinks=False) or entry.is_dir(follow_symlinks=False),
                        'special artifact tree member')
                found.append(path)
                if entry.is_dir(follow_symlinks=False):
                    pending.append(path)
    return sorted(found)


def preflight_image_archives(directory):
    # The existing image-integrity validator also opens outer TARs. Bound those
    # frames before invoking any stdlib archive parser in that earlier stage.
    for label in ('app', 'proxy', 'postgres'):
        with open_regular(Path(directory) / (label + '-image.tar'), ARCHIVE_LIMIT) as raw:
            with validated_tar(raw):
                pass


def regular_inventory(directory, prefix):
    directory = Path(directory)
    require(directory.is_dir() and not directory.is_symlink(), 'missing build artifact directory')
    result, total, count = {}, 0, 0
    for path in bounded_paths(directory):
        count += 1
        require(count <= MAX_MEMBERS and not path.is_symlink(), 'build artifact member budget exceeded')
        if path.is_file():
            name = path.relative_to(directory).as_posix()
            safe_member(name)
            require(path.name not in FORBIDDEN_NAMES, 'private file in build artifacts')
            size = path.stat().st_size
            total += size
            require(size <= LAYER_LIMIT and total <= ARCHIVE_LIMIT, 'build artifact byte budget exceeded')
            result[prefix + name] = file_hash(path)
        else:
            require(path.is_dir(), 'unsupported build artifact type')
    require(result, 'empty build artifact directory')
    return result


def fresh_build_inventory(source, revision, work, run, go):
    """Reproduce actual binary/web artifacts before binding image contents.

    Reuse only the binary's bounded, allowlisted -X release constants, never an
    arbitrary command from build metadata. No install secrets are needed.
    """
    production = source / 'production'
    metadata = read_json(production / 'bin/release.json')
    require(metadata.get('git_commit') == revision, 'local build metadata is stale')
    info = run([go, 'version', '-m', str(production / 'bin/planner')], cwd=production, capture=True).decode()
    flags = None
    settings = {}
    for line in info.splitlines():
        fields = line.strip().split('\t', 1)
        if len(fields) == 2 and fields[0] == 'build' and '=' in fields[1]:
            key, value = fields[1].split('=', 1)
            settings[key] = value
            if key == '-ldflags':
                flags = json.loads(value) if value.startswith('"') else value
    require(isinstance(flags, str) and len(flags) <= 65536, 'missing bounded Go build flags')
    tokens = shlex.split(flags)
    require(tokens[:2] == ['-s', '-w'] and len(tokens[2:]) % 2 == 0, 'unsupported Go build flags')
    prefix = 'github.com/DrToppi/gm-vector/production/internal/planner.'
    allowed = {'embeddedGitCommit', 'embeddedBuildMode', 'communityVersion', 'communityDate',
               'embeddedPublisherKeys', 'embeddedRelease'}
    values = {}
    for flag, assignment in zip(tokens[2::2], tokens[3::2]):
        require(flag == '-X' and assignment.startswith(prefix) and '=' in assignment, 'unsupported Go linker input')
        name, value = assignment[len(prefix):].split('=', 1)
        require(name in allowed and name not in values, 'unsupported release constant')
        values[name] = value
    require(values.get('embeddedGitCommit') == revision, 'binary release constant is stale')
    require(settings.get('CGO_ENABLED') == '0' and settings.get('GOOS') == 'linux'
            and settings.get('GOARCH') == 'amd64', 'unsupported reference build platform')
    env = {key: value for key, value in os.environ.items() if not key.startswith('VITE_')}
    for key in ('GOFLAGS', 'GOEXPERIMENT', 'GOTOOLCHAIN'):
        env.pop(key, None)
    env.update(CGO_ENABLED='0', GOOS='linux', GOARCH='amd64', GOAMD64=settings.get('GOAMD64', 'v1'))
    fresh_binary = work / 'fresh-planner'
    run([go, 'build', '-buildvcs=false', '-trimpath', '-ldflags=' + flags, '-o', str(fresh_binary),
         './cmd/planner'], cwd=production, extra_env=env)
    require(file_hash(fresh_binary) == file_hash(production / 'bin/planner'), 'candidate binary is not a fresh source build')
    require(not list((production / 'web').glob('.env*')), 'local dotenv file in reference web build')
    fresh_web = work / 'fresh-web'
    run(['npm', 'exec', '--offline', '--', 'vite', 'build', '--outDir', str(fresh_web), '--emptyOutDir'],
        cwd=production / 'web', extra_env=env)
    expected_web = regular_inventory(fresh_web, 'app/web/')
    require(expected_web == regular_inventory(production / 'web/dist', 'app/web/'), 'candidate web assets are stale')
    expected = {'app/planner': file_hash(fresh_binary), **expected_web}
    expected.update(regular_inventory(production / 'calendars', 'app/calendars/'))
    expected.update(fresh_notice_inventory(source, work, run, go, env))
    return expected


def fresh_notice_inventory(source, work, run, go, env):
    """Run the exact existing generator with isolated output, not ignored files."""
    production = source / 'production'
    current = regular_inventory(production / 'notices', '')
    require(set(current) == NOTICE_FILES, 'unexpected or missing generated notices')
    staging = work / 'notice-source' / 'production'
    (staging / 'scripts').mkdir(parents=True)
    (staging / 'web').mkdir()
    for name in ('go.mod', 'go.sum', 'scripts/notices.py', 'web/package-lock.json'):
        original = production / name
        require(original.is_file() and not original.is_symlink(), 'missing notice generator input')
        shutil.copyfile(original, staging / name)
    modules = production / 'web/node_modules'
    require(modules.is_dir() and not modules.is_symlink(), 'missing pinned notice dependencies')
    (staging / 'web/node_modules').symlink_to(modules, target_is_directory=True)
    notice_env = dict(env, GOPROXY='off', GOTOOLCHAIN='local')
    notice_env['PATH'] = str(Path(go).parent) + os.pathsep + env.get('PATH', '')
    run([sys.executable, str(staging / 'scripts/notices.py')], cwd=staging,
        extra_env=notice_env, file_limit=LAYER_LIMIT)
    generated = regular_inventory(staging / 'notices', '')
    require(set(generated) == NOTICE_FILES and generated == current, 'generated notices are stale')
    return {'app/notices/' + name: value for name, value in generated.items()}


def distribution_inventory(directory, image_ids):
    """Check actual package membership and byte hashes, not just a source tar."""
    directory = Path(directory)
    require(directory.is_dir() and not directory.is_symlink(), 'missing distribution directory')
    files, total, count = {}, 0, 0
    for path in bounded_paths(directory):
        count += 1
        require(count <= MAX_MEMBERS and not path.is_symlink(), 'unsafe distribution member')
        require(path.is_dir() or path.is_file(), 'special distribution member')
        if path.is_file():
            name = path.relative_to(directory).as_posix()
            safe_member(name)
            require(path.name not in FORBIDDEN_NAMES, 'private distribution member')
            total += path.stat().st_size
            require(total <= ARCHIVE_LIMIT, 'distribution byte budget exceeded')
            if name != 'package.json':
                files[name] = file_hash(path)
    manifest = read_json(directory / 'package.json')
    require(isinstance(manifest, dict) and set(manifest) == {'format', 'images', 'files'}
            and manifest['format'] == 'gm-vector-package-v1', 'malformed distribution manifest')
    require(manifest['images'] == {'app': image_ids['app'], 'proxy': image_ids['proxy'],
                                  'db': image_ids['postgres']}, 'distribution image mismatch')
    require(manifest['files'] == files and
            {'install.py', 'compose.yaml', 'images.tar', 'LICENSE', 'NOTICE'} <= set(files),
            'distribution membership or checksum mismatch')
    return {'package_manifest_sha256': file_hash(directory / 'package.json'),
            'members': len(files) + 1, 'body_bytes': total, 'files': files}


def packaged_distribution(source, work, images, run, env):
    """Use the shipped packager with exact frozen IDs; inspect its real output."""
    ids = {label: images[label]['image_id'] for label in ('app', 'proxy', 'postgres')}
    directory = work / 'distribution'
    run([sys.executable, str(source / 'production/deploy/package.py'),
         '--app-image', ids['app'], '--proxy-image', ids['proxy'],
         '--db-image', ids['postgres'], '--out', str(directory)], extra_env=env, file_limit=ARCHIVE_LIMIT)
    record = distribution_inventory(directory, ids)
    verify_packaged_images(directory / 'images.tar', images)
    archive = work / 'distribution.tar'
    record.update(archive_sha256=distribution_archive(directory, archive,
        {**record['files'], 'package.json': record['package_manifest_sha256']}), image_ids=ids)
    require(distribution_inventory(directory, ids)['files'] == record['files'],
            'distribution changed during inspection')
    return archive, record


def distribution_archive(directory, destination, expected_files=None):
    """The actual hand-out tar: fixed metadata, no links or outside paths."""
    directory, destination = Path(directory), Path(destination)
    require(not destination.resolve().is_relative_to(directory.resolve()), 'archive inside distribution')
    total, count, seen = 0, 0, set()
    with destination.open('xb') as output:
        os.chmod(destination, 0o600)
        class BoundedOutput:
            def tell(self):
                return output.tell()
            def write(self, value):
                require(output.tell() + len(value) <= ARCHIVE_LIMIT, 'distribution archive budget exceeded')
                return output.write(value)
        with tarfile.open(fileobj=BoundedOutput(), mode='w', format=tarfile.PAX_FORMAT) as archive:
            for path in bounded_paths(directory):
                count += 1
                require(count <= MAX_MEMBERS and not path.is_symlink(), 'unsafe distribution member')
                name = path.relative_to(directory).as_posix()
                safe_member(name)
                info = tarfile.TarInfo(name)
                info.mode = 0o755 if path.is_dir() else 0o644
                if path.is_dir():
                    info.type = tarfile.DIRTYPE
                    archive.addfile(info)
                else:
                    require(path.is_file(), 'special distribution member')
                    info.size = path.stat().st_size
                    total += info.size + 1024
                    require(total <= ARCHIVE_LIMIT, 'distribution archive budget exceeded')
                    with path.open('rb') as body:
                        inspected = LimitedObject(body, 'distribution', info.size)
                        # LimitedObject authenticates Git objects with SHA1; the
                        # independent SHA256 below binds the exact tarred bytes.
                        inspected.hash = hashlib.sha256()
                        archive.addfile(info, inspected)
                        require(inspected.remaining == 0, 'truncated distribution file')
                        if expected_files is not None:
                            require(expected_files.get(name) == inspected.hash.hexdigest(),
                                    'distribution changed during archiving')
                            seen.add(name)
                require(output.tell() <= ARCHIVE_LIMIT, 'distribution archive budget exceeded')
        require(output.tell() <= ARCHIVE_LIMIT, 'distribution archive budget exceeded')
    require(expected_files is None or seen == set(expected_files), 'distribution membership changed')
    return file_hash(destination)


def verify_packaged_images(path, images):
    """Bind Docker's combined save to all three previously validated image IDs."""
    with open_regular(path, ARCHIVE_LIMIT) as raw, validated_tar(raw) as checked, tarfile.open(fileobj=checked, mode='r:') as archive:
        members = {}
        for item in archive:
            require(len(members) < 4096, 'distribution image member budget exceeded')
            safe_member(item.name)
            require(item.name not in members and (item.isfile() or item.isdir()),
                    'unsafe distribution image member')
            members[item.name] = item
        def small(name):
            safe_member(name)
            require(name in members and members[name].isfile() and members[name].size <= 32 * 1024 * 1024,
                    'missing distribution image metadata')
            return json_bytes(archive.extractfile(members[name]).read())
        manifest = small('manifest.json')
        require(isinstance(manifest, list) and len(manifest) == len(images), 'distribution image membership mismatch')
        expected = {row['image_id']: row for row in images.values()}
        require(len(expected) == len(images), 'duplicate distribution image identity')
        seen, total = set(), 0
        for entry in manifest:
            require(isinstance(entry, dict), 'malformed distribution image manifest')
            config_name = entry.get('Config'); safe_member(config_name)
            require(config_name in members and members[config_name].isfile()
                    and members[config_name].size <= 32 * 1024 * 1024, 'missing distribution image config')
            config_raw = archive.extractfile(members[config_name]).read()
            identity = 'sha256:' + hashlib.sha256(config_raw).hexdigest()
            require(identity in expected and identity not in seen, 'wrong distribution image config')
            seen.add(identity)
            config = json_bytes(config_raw)
            diff_ids = expected[identity]['diff_ids']
            require(config.get('rootfs', {}).get('type') == 'layers'
                    and config['rootfs'].get('diff_ids') == diff_ids, 'distribution layer identities mismatch')
            names = entry.get('Layers')
            require(isinstance(names, list) and len(names) == len(diff_ids), 'distribution layer membership mismatch')
            for name, expected_hash in zip(names, diff_ids):
                safe_member(name)
                require(name in members and members[name].isfile(), 'missing distribution layer')
                with archive.extractfile(members[name]) as body:
                    prefix = body.read(2); body.seek(0)
                    decoded = gzip.GzipFile(fileobj=body) if prefix == b'\x1f\x8b' else body
                    value, size = hashlib.sha256(), 0
                    try:
                        for chunk in iter(lambda: decoded.read(1024 * 1024), b''):
                            size += len(chunk); total += len(chunk)
                            require(size <= LAYER_LIMIT and total <= ARCHIVE_LIMIT, 'distribution layer budget exceeded')
                            value.update(chunk)
                    finally:
                        if decoded is not body:
                            decoded.close()
                require('sha256:' + value.hexdigest() == expected_hash, 'distribution layer checksum mismatch')
        require(seen == set(expected), 'omitted distribution image')
