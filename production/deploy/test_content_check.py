import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from content_check import inspect_image


def archive_with_layers(path, layers, config=None):
    blobs = {}
    def blob(data):
        digest = hashlib.sha256(data).hexdigest()
        blobs['blobs/sha256/' + digest] = data
        return {'digest': 'sha256:' + digest, 'size': len(data)}
    descriptors = []
    for files in layers:
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode='w') as t:
            for name, data in files.items():
                entry = tarfile.TarInfo(name); entry.size = len(data)
                t.addfile(entry, io.BytesIO(data))
        descriptors.append(blob(output.getvalue()))
    manifest = {'config': blob(json.dumps(config or {}).encode()), 'layers': descriptors}
    image = blob(json.dumps(manifest).encode())['digest']
    with tarfile.open(path, 'w') as t:
        for name, data in blobs.items():
            entry = tarfile.TarInfo(name); entry.size = len(data)
            t.addfile(entry, io.BytesIO(data))
    return image


class ContentCheck(unittest.TestCase):
    def test_public_runtime_and_key_header_are_allowed(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / 'images.tar'
            image = archive_with_layers(p, [{'app/planner': b'ELF -----BEGIN PRIVATE KEY-----',
                'app/web/app.js': b'function run(){}', 'app/public-keys.json': b'{}'}])
            result = inspect_image(p, image)
            self.assertEqual(result['checked_layers'], 1)
            self.assertFalse(result['forbidden_content_found'])

    def test_private_names_and_source_maps_rejected(self):
        for name in ['app/.git/config', 'app/.env', 'app/bootstrap.token', 'app/database.password', 'app/encryption.key']:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                p = Path(tmp) / 'images.tar'; image = archive_with_layers(p, [{name: b'test'}])
                with self.assertRaises(ValueError): inspect_image(p, image)

    def test_complete_private_key_block_rejected_without_echo(self):
        data = b'-----BEGIN PRIVATE KEY-----\nQUJDREVGRw==\n-----END PRIVATE KEY-----'
        for in_config in [False, True]:
            with tempfile.TemporaryDirectory() as tmp:
                p = Path(tmp) / 'images.tar'
                image = archive_with_layers(p, [{'app/innocent.txt': b'ok' if in_config else data}],
                                            {'Env': [data.decode()]} if in_config else None)
                with self.assertRaises(ValueError) as e: inspect_image(p, image)
                self.assertNotIn('QUJD', str(e.exception))

    def test_tampered_layer_digest_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / 'images.tar'; image = archive_with_layers(p, [{'app/a.txt': b'ORIGINAL-CONTENT'}])
            p.write_bytes(p.read_bytes().replace(b'ORIGINAL-CONTENT', b'MODIFIED-CONTENT'))
            with self.assertRaisesRegex(ValueError, 'hash mismatch'): inspect_image(p, image)

    def test_unsafe_path_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / 'images.tar'; image = archive_with_layers(p, [{'../outside': b'test'}])
            with self.assertRaisesRegex(ValueError, 'unsafe layer path'): inspect_image(p, image)

if __name__ == '__main__': unittest.main()
