#!/usr/bin/env python3
"""Read-only release gate for accidental secret inclusion in an OCI app image."""
import argparse
import hashlib
import io
import json
from pathlib import PurePosixPath
import re
import tarfile

MAX_BLOB = 512 * 1024 * 1024
MAX_MEMBER = 256 * 1024 * 1024
MAX_TOTAL = 1024 * 1024 * 1024
PRIVATE_PEM = re.compile(rb'-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----\s+[A-Za-z0-9+/=\r\n]+\s+-----END (?:[A-Z0-9 ]+ )?PRIVATE KEY-----')
SECRET_NAMES = {'signing.key', 'encryption.key', 'bootstrap.token', 'credentials.json',
                'database.password', 'migration-config.json'}


def inspect_image(archive, image):
    if not re.fullmatch(r'sha256:[a-f0-9]{64}', image):
        raise ValueError('invalid image digest')
    findings = set()
    count, total = 0, 0
    with tarfile.open(archive, 'r:*') as outer:
        entries = {}
        for entry in outer:
            if entry.name in entries:
                raise ValueError('duplicate image archive entry')
            entries[entry.name] = entry

        def blob(digest):
            if not isinstance(digest, str) or not re.fullmatch(r'sha256:[a-f0-9]{64}', digest):
                raise ValueError('invalid OCI blob digest')
            entry = entries.get('blobs/sha256/' + digest[7:])
            if entry is None or not entry.isfile() or not 0 <= entry.size <= MAX_BLOB:
                raise ValueError('missing or oversized OCI blob')
            data = outer.extractfile(entry).read(MAX_BLOB + 1)
            if hashlib.sha256(data).hexdigest() != digest[7:]:
                raise ValueError('OCI blob hash mismatch')
            return data

        manifest = json.loads(blob(image))
        layers = manifest.get('layers')
        if not isinstance(layers, list) or not 1 <= len(layers) <= 128:
            raise ValueError('invalid OCI layers')
        # Image config is also part of the deliverable; catch complete PEM data there.
        config = blob(manifest['config']['digest'])
        if PRIVATE_PEM.search(config) or PRIVATE_PEM.search(json.dumps(json.loads(config), ensure_ascii=False).encode().replace(b'\\n', b'\n')):
            findings.add('private_key_in_image_config')
        for layer in layers:
            with tarfile.open(fileobj=io.BytesIO(blob(layer['digest'])), mode='r:*') as source:
                for entry in source:
                    count += 1
                    if count > 100000:
                        raise ValueError('too many layer entries')
                    path = PurePosixPath(entry.name)
                    if path.is_absolute() or '..' in path.parts:
                        raise ValueError('unsafe layer path')
                    parts = path.parts
                    name = path.name.lower()
                    if '.git' in parts or name == '.env' or name.startswith('.env.'):
                        findings.add('repository_or_environment_file')
                    if name in SECRET_NAMES:
                        findings.add('private_installation_or_issuer_file')
                    if not entry.isfile():
                        continue  # Never follow links or extract anything to the filesystem.
                    if not 0 <= entry.size <= MAX_MEMBER:
                        raise ValueError('oversized layer member')
                    total += entry.size
                    if total > MAX_TOTAL:
                        raise ValueError('expanded image exceeds limit')
                    if PRIVATE_PEM.search(source.extractfile(entry).read(MAX_MEMBER + 1)):
                        findings.add('complete_private_key_block')
    if findings:
        # Category names only: no file contents, credentials, or customer paths.
        raise ValueError('release content rejected: ' + ', '.join(sorted(findings)))
    return {'checked_layers': len(layers), 'checked_entries': count,
            'checked_bytes': total, 'forbidden_content_found': False}


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--archive', required=True)
    p.add_argument('--app-image', required=True)
    a = p.parse_args()
    try:
        print(json.dumps(inspect_image(a.archive, a.app_image)))
    except (ValueError, KeyError, TypeError, OSError, tarfile.TarError):
        p.exit(1, 'Release content check failed; inspect publisher input privately.\n')
