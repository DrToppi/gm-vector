#!/usr/bin/env python3
"""Offline signing, manifest and tamper regressions; synthetic temp keys only."""
import json, os, subprocess, tempfile
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
TOOL = ROOT / 'bin/release-verifier'
def invoke(*args, ok=True):
    result = subprocess.run([str(TOOL), *map(str,args)], capture_output=True, text=True)
    assert (result.returncode == 0) == ok, (args[0], result.stdout, result.stderr)
    return result
with tempfile.TemporaryDirectory(prefix='gma-release-test-') as directory:
    temp = Path(directory); issuer = temp/'issuer'; foreign = temp/'foreign'; bundle=temp/'bundle'; bundle.mkdir()
    invoke('keygen','--dir',issuer); invoke('keygen','--dir',foreign)
    metadata = temp/'release.json'
    invoke('sign-release','--key',issuer/'signing.key','--version','1.2.3','--date','2026-10-28','--commit','a'*40,'--out',metadata)
    invoke('verify-release','--keys',issuer/'public-keys.json','--file',metadata)
    invoke('verify-release','--keys',foreign/'public-keys.json','--file',metadata,ok=False)
    (bundle/'planner').write_bytes(b'synthetic distribution binary')
    (bundle/'web').mkdir(); (bundle/'web/index.html').write_text('<html>synthetic</html>')
    invoke('sign-bundle','--dir',bundle,'--file',metadata,'--keys',issuer/'public-keys.json','--key',issuer/'signing.key')
    verify = ('verify-bundle','--dir',bundle,'--keys',issuer/'public-keys.json')
    invoke(*verify)
    invoke('verify-bundle','--dir',bundle,'--keys',foreign/'public-keys.json',ok=False)
    (bundle/'planner').write_bytes(b'tampered'); invoke(*verify,ok=False)
    (bundle/'planner').write_bytes(b'synthetic distribution binary'); invoke(*verify)
    (bundle/'extra').write_text('unlisted'); invoke(*verify,ok=False); (bundle/'extra').unlink()
    original=(bundle/'web/index.html').read_bytes(); (bundle/'web/index.html').unlink(); invoke(*verify,ok=False)
    (bundle/'web/index.html').symlink_to(metadata); invoke(*verify,ok=False)
    (bundle/'web/index.html').unlink(); (bundle/'web/index.html').write_bytes(original)
    manifest=bundle/'signed-manifest.json'; signed=manifest.read_bytes(); envelope=json.loads(signed); envelope['signature']='A'*86; manifest.write_text(json.dumps(envelope)); invoke(*verify,ok=False); manifest.write_bytes(signed)
    # A legitimate synthetic envelope still cannot pass production verification.
    synthetic=temp/'synthetic.json'
    invoke('sign-release','--key',issuer/'signing.key','--version','1.2.3-synthetic','--date','2026-10-28','--commit','a'*40,'--channel','synthetic','--out',synthetic)
    invoke('verify-release','--keys',issuer/'public-keys.json','--file',synthetic,ok=False)
    invoke('verify-release','--keys',issuer/'public-keys.json','--file',synthetic,'--allow-synthetic')
    manifest.unlink(); invoke('sign-bundle','--dir',bundle,'--file',synthetic,'--keys',issuer/'public-keys.json','--key',issuer/'signing.key'); invoke(*verify,ok=False); invoke(*verify,'--allow-synthetic')
    assert oct((issuer/'signing.key').stat().st_mode & 0o777)=='0o600'
print('Release PASS: signature/root/context, inventory/hash/add/remove/symlink tamper, production rejects synthetic; temporary private keys only.')
