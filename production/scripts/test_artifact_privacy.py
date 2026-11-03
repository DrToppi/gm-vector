"""Independent inert archive oracles for all-layer/privacy traversal."""
import io
import gzip
import hashlib
import tarfile
import unittest
import importlib.util
import json
import shutil
import struct
from pathlib import Path
import tempfile
from types import SimpleNamespace
from unittest import mock

import artifact_privacy as privacy
import zipfile

from artifact_privacy import ApplicationAssets, ContentStream, walk_tar, walk_git_batch
from image_gate import GateError


def archive_bytes(members):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w') as archive:
        for name, data, kind, target in members:
            info = tarfile.TarInfo(name)
            info.type = kind
            info.linkname = target
            info.size = len(data)
            archive.addfile(info, io.BytesIO(data))
    output.seek(0)
    return output


def file(name, data):
    return name, data, tarfile.REGTYPE, ''


class ArtifactPrivacyTests(unittest.TestCase):
    def test_binary_and_duplicate_and_deleted_bodies_are_all_inspected(self):
        output = io.BytesIO()
        stream = ContentStream(output)
        assets = ApplicationAssets()
        for members in ([file('app/planner', b'\x7fELF\x00FIRST_INERT_MARKER'),
                         file('app/planner', b'SECOND_INERT_MARKER')],
                        [file('app/.wh.planner', b'')]):
            observe, finish = assets.layer()
            walk_tar(archive_bytes(members), stream, observe)
            finish()
        self.assertIn(b'FIRST_INERT_MARKER', output.getvalue())
        self.assertIn(b'SECOND_INERT_MARKER', output.getvalue())
        self.assertEqual(stream.receipt()['members'], 3)
        self.assertEqual(assets.files, {})

    def test_whiteout_applies_to_lower_layer_before_current_additions(self):
        stream = ContentStream(io.BytesIO())
        assets = ApplicationAssets()
        for members in ([file('app/web/old.js', b'old')],
                        [file('app/web/new.js', b'new'), file('app/web/.wh..wh..opq', b'')]):
            observe, finish = assets.layer()
            walk_tar(archive_bytes(members), stream, observe)
            finish()
        self.assertEqual(set(assets.files), {'app/web/new.js'})

    def test_archive_links_are_metadata_never_followed(self):
        output = io.BytesIO()
        stream = ContentStream(output)
        walk_tar(archive_bytes([('usr/bin/sh', b'', tarfile.SYMTYPE, '/bin/busybox'),
                               ('usr/bin/alias', b'', tarfile.LNKTYPE, '/outside/PRIVATE_INERT_TARGET')]), stream)
        self.assertEqual(stream.receipt()['body_bytes'], 0)
        self.assertIn(b'PRIVATE_INERT_TARGET', output.getvalue())
        self.assertEqual(stream.receipt()['members'], 2)

    def test_unsafe_names_and_private_installation_files_fail(self):
        for name in ('../outside', '/absolute', 'app/encryption.key', 'nested/credentials.json'):
            with self.subTest(name=name), self.assertRaises(GateError):
                walk_tar(archive_bytes([file(name, b'inert')]), ContentStream(io.BytesIO()))

    def test_truncated_malformed_empty_and_oversized_content_fail(self):
        for value in (b'invalid tar', b'', archive_bytes([file('app/planner', b'x' * 1000)]).read()[:513]):
            with self.subTest(size=len(value)), self.assertRaises(GateError):
                walk_tar(io.BytesIO(value), ContentStream(io.BytesIO()))
        with self.assertRaises(GateError):
            ContentStream(io.BytesIO()).consume('x', {}, io.BytesIO(b'xx'), 1)

    def test_pax_metadata_is_inspected(self):
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode='w', format=tarfile.PAX_FORMAT) as archive:
            info = tarfile.TarInfo('app/file')
            info.pax_headers = {'SCHILY.xattr.user.note': 'INERT_XATTR_MARKER'}
            archive.addfile(info, io.BytesIO())
        output.seek(0)
        contents = io.BytesIO()
        walk_tar(output, ContentStream(contents))
        self.assertIn(b'INERT_XATTR_MARKER', contents.getvalue())

    def test_nested_gzip_and_zip_bodies_are_decoded_without_extraction(self):
        zipped = io.BytesIO()
        with zipfile.ZipFile(zipped, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
            archive.writestr('notes.txt', b'NESTED_INERT_MARKER')
        output = io.BytesIO()
        stream = ContentStream(output)
        walk_tar(archive_bytes([file('assets.zip.gz', gzip.compress(zipped.getvalue(), mtime=0))]), stream)
        self.assertIn(b'NESTED_INERT_MARKER', output.getvalue())
        self.assertEqual(stream.members, 3)

    def test_nested_unsafe_path_depth_and_unsupported_compression_block(self):
        zipped = io.BytesIO()
        with zipfile.ZipFile(zipped, 'w') as archive:
            archive.writestr('../outside', b'inert')
        with self.assertRaises(GateError):
            walk_tar(archive_bytes([file('assets.zip', zipped.getvalue())]), ContentStream(io.BytesIO()))
        value = b'inert'
        for _ in range(7):
            value = gzip.compress(value, mtime=0)
        with self.assertRaises(GateError):
            walk_tar(archive_bytes([file('deep.gz', value)]), ContentStream(io.BytesIO()))
        with self.assertRaises(GateError):
            walk_tar(archive_bytes([file('opaque.zst', b'\x28\xb5\x2f\xfdopaque')]), ContentStream(io.BytesIO()))

    def test_git_history_inspects_binary_blobs_and_commit_messages(self):
        objects = [('blob', b'\x7fELF\0HISTORICAL_INERT_MARKER'), ('commit', b'COMMIT_INERT_MARKER')]
        wire, ids = b'', []
        for kind, raw in objects:
            oid = hashlib.sha1((kind + ' ' + str(len(raw)) + '\0').encode() + raw).hexdigest()
            ids.append(oid)
            wire += (oid + ' ' + kind + ' ' + str(len(raw)) + '\n').encode() + raw + b'\n'
        output = io.BytesIO()
        self.assertEqual(walk_git_batch(io.BytesIO(wire), ContentStream(output), ids), 2)
        self.assertIn(b'HISTORICAL_INERT_MARKER', output.getvalue())
        self.assertIn(b'COMMIT_INERT_MARKER', output.getvalue())
        with self.assertRaises(GateError):
            walk_git_batch(io.BytesIO(wire[:-2]), ContentStream(io.BytesIO()), ids)
        with self.assertRaises(GateError):
            walk_git_batch(io.BytesIO(wire), ContentStream(io.BytesIO()), ids[:-1])

    def test_private_filename_in_historical_tree_is_rejected(self):
        raw = b'100644 credentials.json\0' + b'\0' * 20
        oid = hashlib.sha1(b'tree ' + str(len(raw)).encode() + b'\0' + raw).hexdigest()
        wire = (oid + ' tree ' + str(len(raw)) + '\n').encode() + raw + b'\n'
        with self.assertRaises(GateError):
            walk_git_batch(io.BytesIO(wire), ContentStream(io.BytesIO()), [oid])


class PrivacyBoundaryRegressionTests(unittest.TestCase):
    def test_nonzero_tar_trailer_cannot_hide_uninspected_content(self):
        raw = archive_bytes([file('public.txt', b'public')]).getvalue()
        for tail in (b'UNINSPECTED_TRAILER', archive_bytes([file('hidden.txt', b'hidden')]).getvalue()):
            with self.subTest(tail_size=len(tail)), self.assertRaises(GateError):
                walk_tar(io.BytesIO(raw + tail), ContentStream(io.BytesIO()))

    def test_extended_tar_header_rejected_before_large_body_read(self):
        info = tarfile.TarInfo('extended')
        info.type = tarfile.XHDTYPE
        info.size = 2 * 1024 * 1024
        class ReadFence(io.BytesIO):
            def read(self, size=-1):
                if self.tell() >= 512 or size > 10240:
                    raise AssertionError('unbounded extension body requested')
                return super().read(size)
        with self.assertRaises(GateError):
            walk_tar(ReadFence(info.tobuf()), ContentStream(io.BytesIO()))

    def test_serialized_detector_stream_includes_metadata_in_budget(self):
        with mock.patch.object(privacy, 'ARCHIVE_LIMIT', 256):
            with self.assertRaises(GateError):
                ContentStream(io.BytesIO()).consume('public.txt', {'note': 'x' * 512})

    def test_app_link_or_special_parent_is_never_a_valid_runtime_layout(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.FIFOTYPE):
            assets = ApplicationAssets()
            stream = ContentStream(io.BytesIO())
            observe, finish = assets.layer()
            with self.subTest(kind=kind), self.assertRaises(GateError):
                walk_tar(archive_bytes([file('app/web/public.js', b'old'),
                         ('app/web', b'', kind, '/outside')]), stream, observe)
                finish()

    def test_actual_checker_checkout_cannot_contain_private_output(self):
        script = Path(__file__).with_name('artifact-privacy-check.py')
        spec = importlib.util.spec_from_file_location('privacy_cli_for_test', script)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as tmp:
            args = SimpleNamespace(source=Path(tmp), output_dir=script.parents[2] / 'private-inert-output',
                                   source_revision='a' * 40)
            with mock.patch.object(Path, 'mkdir', side_effect=AssertionError('output creation reached')):
                with self.assertRaises(GateError):
                    module.produce(args)

    def test_stale_or_extra_notices_do_not_count_as_fresh_build(self):
        revision = 'a' * 40
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); source = root / 'source'; work = root / 'work'; work.mkdir()
            production = source / 'production'
            for name in ('bin', 'web/dist', 'calendars', 'notices'):
                (production / name).mkdir(parents=True)
            (production / 'bin/release.json').write_text(json.dumps({'git_commit': revision}))
            (production / 'bin/planner').write_bytes(b'fresh-binary')
            (production / 'web/dist/public.js').write_bytes(b'fresh-web')
            (production / 'calendars/public.json').write_bytes(b'{}')
            (production / 'notices/old-private.txt').write_bytes(b'stale-notice')
            prefix = 'github.com/DrToppi/gm-vector/production/internal/planner.'
            flags = '-s -w -X ' + prefix + 'embeddedGitCommit=' + revision
            def run(command, **kwargs):
                if command[1:3] == ['version', '-m']:
                    return ('build\t-ldflags=' + json.dumps(flags) + '\nbuild\tCGO_ENABLED=0\nbuild\tGOOS=linux\nbuild\tGOARCH=amd64\n').encode()
                if command[1] == 'build':
                    Path(command[command.index('-o') + 1]).write_bytes(b'fresh-binary')
                elif command[0] == 'npm':
                    out = Path(command[command.index('--outDir') + 1]); out.mkdir()
                    (out / 'public.js').write_bytes(b'fresh-web')
                return b''
            with self.assertRaises(GateError):
                privacy.fresh_build_inventory(source, revision, work, run, '/synthetic/go')


def packaged_fixture(directory):
    """Three independent immutable configs and one shared inert tar layer."""
    directory.mkdir()
    layer = archive_bytes([file('public.txt', b'PACKAGED_LAYER_CONTENT')]).getvalue()
    layer_digest = 'sha256:' + hashlib.sha256(layer).hexdigest()
    manifest, entries, images = [], [file('layers/shared.tar', layer)], {}
    for role in ('app', 'proxy', 'postgres'):
        config = json.dumps({'architecture': role, 'rootfs': {'type': 'layers',
                            'diff_ids': [layer_digest]}}, sort_keys=True).encode()
        identity = 'sha256:' + hashlib.sha256(config).hexdigest()
        name = identity[7:] + '.json'
        entries.append(file(name, config))
        manifest.append({'Config': name, 'Layers': ['layers/shared.tar'], 'RepoTags': None})
        images[role] = {'image_id': identity, 'diff_ids': [layer_digest]}
    entries.append(file('manifest.json', json.dumps(manifest).encode()))
    (directory / 'images.tar').write_bytes(archive_bytes(entries).getvalue())
    for name in ('install.py', 'compose.yaml', 'LICENSE', 'NOTICE'):
        (directory / name).write_text('PUBLIC_DISTRIBUTION_CONTENT')
    ids = {key: row['image_id'] for key, row in images.items()}
    files = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.iterdir()}
    (directory / 'package.json').write_text(json.dumps({'format': 'gm-vector-package-v1',
        'images': {'app': ids['app'], 'proxy': ids['proxy'], 'db': ids['postgres']}, 'files': files}))
    return images


class PrivacyDistributionTests(unittest.TestCase):
    def test_tar_header_and_file_padding_are_detector_input(self):
        output = io.BytesIO()
        info = tarfile.TarInfo('public.txt'); info.size = 1; info.uname = 'HEADER_INERT_MARKER'
        payload = info.tobuf() + b'x' + b'PADDING_INERT_MARKER'.ljust(511, b'\0') + b'\0' * 1024
        walk_tar(io.BytesIO(payload), ContentStream(output))
        self.assertIn(b'HEADER_INERT_MARKER', output.getvalue())
        self.assertIn(b'PADDING_INERT_MARKER', output.getvalue())

    def test_extension_aggregate_and_sparse_payload_are_bounded(self):
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode='w', format=tarfile.PAX_FORMAT) as archive:
            for n in range(2):
                info = tarfile.TarInfo('public' + str(n)); info.pax_headers = {'comment': 'x' * 500}
                archive.addfile(info)
        with mock.patch.object(privacy, 'MAX_EXTENSIONS', 700):
            with self.assertRaises(GateError):
                walk_tar(io.BytesIO(output.getvalue()), ContentStream(io.BytesIO()))
        info = tarfile.TarInfo('public'); info.type = tarfile.XHDTYPE
        data = b'40 GNU.sparse.map=0,1\n'; info.size = len(data)
        with self.assertRaises(GateError):
            walk_tar(io.BytesIO(info.tobuf() + data.ljust(512, b'\0') + b'\0' * 1024),
                     ContentStream(io.BytesIO()))

    def test_named_prefixed_zip_is_rejected_before_compressed_members_can_hide(self):
        zipped = io.BytesIO()
        with zipfile.ZipFile(zipped, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
            archive.writestr('public.txt', b'PREFIXED_COMPRESSED_INERT_CONTENT')
        payload = b'INERT_SELF_EXTRACTING_PREFIX' + zipped.getvalue()
        # Independent oracle: this is valid ZIP input for the standard library,
        # despite its unsupported executable/prefixed leading layout.
        with zipfile.ZipFile(io.BytesIO(payload)) as archive:
            self.assertEqual(archive.read('public.txt'), b'PREFIXED_COMPRESSED_INERT_CONTENT')
        for name in ('archive.zip', 'ARCHIVE.ZIP'):
            with self.subTest(name=name), self.assertRaises(GateError):
                ContentStream(io.BytesIO()).consume(name, {}, io.BytesIO(payload), len(payload))

    def test_zip_directory_bound_checked_before_stdlib_allocation(self):
        tail = struct.pack('<4s4H2IH', b'PK\x05\x06', 0, 0, 1, 1, 40 * 1024 * 1024, 0, 0)
        value = b'PK\x03\x04' + tail
        with mock.patch.object(privacy.zipfile, 'ZipFile', side_effect=AssertionError('stdlib allocation')):
            with self.assertRaises(GateError):
                ContentStream(io.BytesIO()).consume('inert.zip', {}, io.BytesIO(value), len(value))

    def test_known_database_or_backup_prefix_is_never_distributed(self):
        for prefix in (b'GMABK01', b'PGDMP', b'SQLite format 3\x00'):
            with self.subTest(prefix=prefix), self.assertRaises(GateError):
                ContentStream(io.BytesIO()).consume('public.bin', {}, io.BytesIO(prefix), len(prefix))

    def test_runtime_regular_file_cannot_be_parent_of_another_asset(self):
        assets = ApplicationAssets(); observe, finish = assets.layer()
        with self.assertRaises(GateError):
            walk_tar(archive_bytes([file('app/web/public.js', b'child'), file('app/web', b'parent')]),
                     ContentStream(io.BytesIO()), observe)
            finish()

    def test_exact_package_and_images_are_bound_and_archive_inspected_once(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); package = root / 'package'; images = packaged_fixture(package)
            ids = {k: v['image_id'] for k, v in images.items()}
            record = privacy.distribution_inventory(package, ids)
            privacy.verify_packaged_images(package / 'images.tar', images)
            outer = root / 'distribution.tar'
            expected = {**record['files'], 'package.json': record['package_manifest_sha256']}
            privacy.distribution_archive(package, outer, expected)
            output = io.BytesIO()
            with outer.open('rb') as body:
                stream = ContentStream(output)
                stream.consume('distribution.tar', {}, body, outer.stat().st_size)
            # Combined save has one shared layer. It is visited once recursively,
            # independent of the three image manifest references to that layer.
            self.assertEqual(stream.members, 13)
            self.assertIn(b'PACKAGED_LAYER_CONTENT', output.getvalue())
            with tarfile.open(outer) as tar:
                self.assertEqual({m.name for m in tar if m.isfile()}, set(expected))
                for member in tar:
                    self.assertEqual((member.uid, member.gid, member.uname, member.gname, member.mtime),
                                     (0, 0, '', '', 0))

    def test_package_omission_extra_changed_hash_wrong_role_and_symlink_block(self):
        for change in ('omitted', 'extra', 'changed', 'wrong-image', 'symlink'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as tmp:
                package = Path(tmp) / 'package'; images = packaged_fixture(package)
                ids = {k: v['image_id'] for k, v in images.items()}
                manifest = json.loads((package / 'package.json').read_text())
                if change == 'omitted':
                    del manifest['files']['images.tar']
                elif change == 'extra':
                    (package / 'unlisted.txt').write_text('inert')
                elif change == 'changed':
                    (package / 'install.py').write_text('changed')
                elif change == 'wrong-image':
                    manifest['images']['db'] = ids['app']
                else:
                    (package / 'alias').symlink_to(package / 'LICENSE')
                (package / 'package.json').write_text(json.dumps(manifest))
                with self.assertRaises(GateError):
                    privacy.distribution_inventory(package, ids)

    def test_swapped_or_mutated_packaged_image_layer_blocks(self):
        with tempfile.TemporaryDirectory() as tmp:
            package = Path(tmp) / 'package'; images = packaged_fixture(package)
            changed = {k: dict(v) for k, v in images.items()}
            changed['app']['image_id'] = 'sha256:' + '0' * 64
            with self.assertRaises(GateError):
                privacy.verify_packaged_images(package / 'images.tar', changed)
            raw = (package / 'images.tar').read_bytes().replace(b'PACKAGED_LAYER_CONTENT', b'ALTERED_LAYER_CONTENT!')
            self.assertNotEqual(raw, (package / 'images.tar').read_bytes())
            (package / 'images.tar').write_bytes(raw)
            with self.assertRaises(GateError):
                privacy.verify_packaged_images(package / 'images.tar', images)

    def test_modified_package_between_verification_and_archiving_blocks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); package = root / 'package'; images = packaged_fixture(package)
            record = privacy.distribution_inventory(package, {k: v['image_id'] for k, v in images.items()})
            (package / 'install.py').write_text('changed')
            with self.assertRaises(GateError):
                privacy.distribution_archive(package, root / 'distribution.tar',
                    {**record['files'], 'package.json': record['package_manifest_sha256']})

    def test_actual_packager_command_uses_exact_ids_and_db_mapping(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); original = root / 'original'; images = packaged_fixture(original)
            source = root / 'source'; work = root / 'work'; work.mkdir()
            commands = []
            def run(command, **kwargs):
                commands.append(command)
                self.assertEqual(kwargs['extra_env'], {'DOCKER_HOST': 'unix:///inert/socket'})
                shutil.copytree(original, Path(command[command.index('--out') + 1]))
            archive, record = privacy.packaged_distribution(source, work, images, run,
                                                            {'DOCKER_HOST': 'unix:///inert/socket'})
            command = commands[0]
            self.assertEqual(Path(command[1]), source / 'production/deploy/package.py')
            for flag, role in (('--app-image', 'app'), ('--proxy-image', 'proxy'), ('--db-image', 'postgres')):
                self.assertEqual(command[command.index(flag) + 1], images[role]['image_id'])
            self.assertEqual(archive, work / 'distribution.tar')
            self.assertEqual(record['archive_sha256'], hashlib.sha256(archive.read_bytes()).hexdigest())

    def test_notices_are_regenerated_isolated_and_stale_bytes_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); source = root / 'source'; production = source / 'production'
            work = root / 'work'; work.mkdir()
            for name in ('scripts', 'web/node_modules', 'notices'):
                (production / name).mkdir(parents=True)
            for name in ('go.mod', 'go.sum', 'scripts/notices.py', 'web/package-lock.json'):
                (production / name).write_text('INERT_GENERATOR_INPUT')
            expected = {'THIRD-PARTY-NOTICES.txt': b'fresh-notice', 'inventory.json': b'[]\n'}
            for name, value in expected.items():
                (production / 'notices' / name).write_bytes(value)
            calls = []
            def run(command, **kwargs):
                calls.append(command)
                stage = Path(command[1]).parents[1]
                self.assertNotEqual(stage, production)
                self.assertEqual((stage / 'scripts/notices.py').read_bytes(),
                                 (production / 'scripts/notices.py').read_bytes())
                self.assertEqual((stage / 'web/node_modules').resolve(), production / 'web/node_modules')
                self.assertEqual(kwargs['extra_env']['GOPROXY'], 'off')
                self.assertEqual(kwargs['extra_env']['GOTOOLCHAIN'], 'local')
                (stage / 'notices').mkdir()
                for name, value in expected.items():
                    (stage / 'notices' / name).write_bytes(value)
            result = privacy.fresh_notice_inventory(source, work, run, '/inert/go', {'PATH': '/bin'})
            self.assertEqual(set(result), {'app/notices/' + name for name in expected})
            self.assertEqual(len(calls), 1)
            (production / 'notices/inventory.json').write_bytes(b'stale')
            other = root / 'other'; other.mkdir()
            with self.assertRaises(GateError):
                privacy.fresh_notice_inventory(source, other, run, '/inert/go', {'PATH': '/bin'})
            (production / 'notices/extra.txt').write_text('unexpected')
            with self.assertRaises(GateError):
                privacy.fresh_notice_inventory(source, root / 'unused', run, '/inert/go', {'PATH': '/bin'})
