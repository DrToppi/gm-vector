"""Strict, offline validation of the WP-05 three-image scan receipt.

The producer is trusted CI code; this parser does not authenticate an arbitrary
manifest supplied by an attacker. D14 exception admission is deliberately absent.
"""
import datetime as dt
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import tarfile

SCANNER = 'aquasec/trivy@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c'
VERSION = '0.76.1'
LABELS = ('app', 'proxy', 'postgres')
SEVERITIES = ('UNKNOWN', 'LOW', 'MEDIUM', 'HIGH', 'CRITICAL')
JSON_LIMIT = 32 * 1024 * 1024
ARCHIVE_LIMIT = 2 * 1024 * 1024 * 1024
LAYER_LIMIT = 512 * 1024 * 1024
DIGEST = re.compile(r'sha256:[0-9a-f]{64}\Z')
REVISION = re.compile(r'[0-9a-f]{40}\Z')


class GateError(ValueError):
    """Only static messages reach the public log; never echo malformed input."""


def require(condition, message):
    if not condition:
        raise GateError(message)


def sha256_bytes(value):
    return hashlib.sha256(value).hexdigest()


def file_hash(path):
    digest = hashlib.sha256()
    with open_regular(path, ARCHIVE_LIMIT) as stream:
        for part in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(part)
    return digest.hexdigest()


def open_regular(path, limit):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= limit,
                'missing, empty, nonregular or oversized evidence')
        return os.fdopen(fd, 'rb')
    except BaseException:
        os.close(fd)
        raise


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, 'duplicate JSON field')
        value[key] = item
    return value


def json_bytes(raw):
    require(0 < len(raw) <= JSON_LIMIT, 'empty or oversized JSON')
    try:
        return json.loads(raw, object_pairs_hook=unique_object,
                          parse_constant=lambda _: (_ for _ in ()).throw(GateError('invalid JSON number')))
    except (UnicodeError, json.JSONDecodeError, RecursionError) as exc:
        raise GateError('malformed JSON evidence') from exc


def read_json(path):
    with open_regular(path, JSON_LIMIT) as stream:
        return json_bytes(stream.read(JSON_LIMIT + 1))


def timestamp(raw):
    require(isinstance(raw, str) and len(raw) <= 64, 'missing timestamp')
    try:
        value = dt.datetime.fromisoformat(raw.replace('Z', '+00:00'))
    except ValueError as exc:
        raise GateError('malformed timestamp') from exc
    require(value.tzinfo is not None and value.utcoffset() is not None,
            'timestamp must include timezone')
    try:
        return value.astimezone(dt.timezone.utc)
    except (OverflowError, ValueError) as exc:
        raise GateError('timestamp outside supported range') from exc


def validate_db(metadata, now):
    require(isinstance(metadata, dict) and metadata.get('Version') == 2,
            'unsupported advisory database metadata')
    updated = timestamp(metadata.get('UpdatedAt'))
    age = (now - updated).total_seconds()
    require(0 <= age <= 24 * 3600, 'advisory database is future-dated or stale')
    return updated


def safe_member(name):
    require(isinstance(name, str) and len(name) <= 1024 and '\\' not in name,
            'unsafe archive member')
    path = PurePosixPath(name)
    require(name and not path.is_absolute() and '..' not in path.parts,
            'unsafe archive member')
    return name


def layer_stream(stream):
    """Read either Docker tar layers or OCI gzip layers without extraction."""
    prefix = stream.read(2)
    stream.seek(0)
    return gzip.GzipFile(fileobj=stream) if prefix == b'\x1f\x8b' else stream


def archive_inventory(path, image_id, diff_ids):
    require(isinstance(image_id, str) and DIGEST.fullmatch(image_id), 'invalid frozen image ID')
    require(isinstance(diff_ids, list) and diff_ids and len(diff_ids) <= 256
            and all(isinstance(x, str) and DIGEST.fullmatch(x) for x in diff_ids),
            'invalid frozen layer IDs')
    with open_regular(path, ARCHIVE_LIMIT) as source, tarfile.open(fileobj=source, mode='r:') as archive:
        members = {}
        for item in archive:
            require(len(members) < 4096, 'too many image archive members')
            safe_member(item.name)
            require(item.name not in members and (item.isfile() or item.isdir()),
                    'duplicate or unsafe image archive member')
            require(0 <= item.size <= ARCHIVE_LIMIT, 'oversized image archive member')
            members[item.name] = item

        def small(name):
            require(name in members and members[name].isfile() and members[name].size <= JSON_LIMIT,
                    'missing image archive metadata')
            return archive.extractfile(members[name]).read(JSON_LIMIT + 1)

        manifest = json_bytes(small('manifest.json'))
        require(isinstance(manifest, list) and len(manifest) == 1,
                'image archive must contain exactly one image')
        entry = manifest[0]
        require(isinstance(entry, dict), 'malformed image archive manifest')
        config_name = safe_member(entry.get('Config'))
        config_raw = small(config_name)
        require('sha256:' + sha256_bytes(config_raw) == image_id, 'saved config does not match frozen image')
        config = json_bytes(config_raw)
        require(config.get('rootfs', {}).get('type') == 'layers'
                and config['rootfs'].get('diff_ids') == diff_ids,
                'saved image layer inventory mismatch')
        names = entry.get('Layers')
        require(isinstance(names, list) and len(names) == len(diff_ids), 'saved layer membership mismatch')
        layers = []
        total = 0
        for name, expected in zip(names, diff_ids):
            safe_member(name)
            require(name in members and members[name].isfile(), 'missing saved layer')
            digest, size = hashlib.sha256(), 0
            with archive.extractfile(members[name]) as raw:
                payload = layer_stream(raw)
                try:
                    for part in iter(lambda: payload.read(1024 * 1024), b''):
                        size += len(part)
                        total += len(part)
                        require(size <= LAYER_LIMIT and total <= ARCHIVE_LIMIT, 'expanded layer budget exceeded')
                        digest.update(part)
                finally:
                    if payload is not raw:
                        payload.close()
            require('sha256:' + digest.hexdigest() == expected, 'saved layer content digest mismatch')
            layers.append({'member': name, 'diff_id': expected, 'expanded_bytes': size})
        return {'image_id': image_id, 'diff_ids': diff_ids, 'layers': layers,
                'archive_sha256': file_hash(path), 'config_sha256': sha256_bytes(config_raw)}


def validate_reports(label, image, report, sbom, started, finished, db_updated):
    require(isinstance(report, dict) and report.get('SchemaVersion') == 2
            and report.get('ArtifactType') == 'container_image'
            and report.get('Trivy', {}).get('Version') == VERSION,
            'unsupported or incomplete vulnerability report')
    created = timestamp(report.get('CreatedAt'))
    require(started <= created <= finished and db_updated <= created,
            'report does not belong to scan/advisory epoch')
    metadata = report.get('Metadata', {})
    require(metadata.get('ImageID') == image['image_id']
            and metadata.get('DiffIDs') == image['diff_ids'], 'report image identity mismatch')
    family = 'debian' if label == 'app' else 'alpine'
    require(metadata.get('OS', {}).get('Family') == family, 'unexpected or missing OS identity')
    results = report.get('Results')
    require(isinstance(results, list) and results, 'missing vulnerability result inventory')
    packages, classes, findings = set(), set(), []
    targets = set()
    for result in results:
        require(isinstance(result, dict) and result.get('Class') in ('os-pkgs', 'lang-pkgs'),
                'unsupported result class')
        key = (result.get('Class'), result.get('Type'), result.get('Target'))
        require(all(isinstance(x, str) and x for x in key) and key not in targets,
                'missing or duplicated result target')
        targets.add(key)
        classes.add(key[:2])
        supported = {('os-pkgs', family)} | ({('lang-pkgs', 'gobinary')} if label == 'app' else set())
        require(key[:2] in supported, 'unsupported package result type')
        if key[0] == 'lang-pkgs':
            require(key[2].lstrip('/') == 'app/planner', 'application binary target omitted or unexpected')
        inventory = result.get('Packages')
        require(isinstance(inventory, list) and inventory, 'missing package inventory')
        for package in inventory:
            require(isinstance(package, dict) and isinstance(package.get('Name'), str),
                    'invalid package inventory')
            purl = package.get('Identifier', {}).get('PURL')
            require(isinstance(purl, str) and purl.startswith('pkg:'), 'missing package identity')
            packages.add(purl)
        vulnerabilities = result.get('Vulnerabilities', [])
        require(isinstance(vulnerabilities, list), 'invalid vulnerability list')
        for item in vulnerabilities:
            require(isinstance(item, dict) and item.get('Severity') in SEVERITIES,
                    'missing or invalid finding severity')
            require(all(isinstance(item.get(x), str) and item[x]
                        for x in ('VulnerabilityID', 'PkgName', 'InstalledVersion')),
                    'missing finding identity')
            findings.append({'target': key[2], 'id': item['VulnerabilityID'],
                             'package': item['PkgName'], 'version': item['InstalledVersion'],
                             'severity': item['Severity'], 'fixed_version': item.get('FixedVersion', '')})
        require(not result.get('ExperimentalModifiedFindings'), 'suppressed findings are not admitted')
    require(('os-pkgs', family) in classes, 'OS package scan omitted')
    if label == 'app':
        require(('lang-pkgs', 'gobinary') in classes, 'application Go package scan omitted')

    require(isinstance(sbom, dict) and sbom.get('bomFormat') == 'CycloneDX'
            and sbom.get('specVersion') == '1.7', 'unsupported SBOM')
    meta = sbom.get('metadata', {})
    require(meta.get('component', {}).get('type') == 'container', 'SBOM root is not a container')
    converted = timestamp(meta.get('timestamp'))
    conversion_start = timestamp(image.get('conversion_started_at'))
    conversion_end = timestamp(image.get('conversion_finished_at'))
    require(started <= conversion_start <= conversion_end <= finished
            and conversion_start.replace(microsecond=0) <= converted <= conversion_end,
            'SBOM does not belong to conversion window')
    require(any(x.get('name') == 'trivy' and x.get('version') == VERSION
                for x in meta.get('tools', {}).get('components', [])), 'SBOM scanner mismatch')
    properties = meta.get('component', {}).get('properties', [])
    require(isinstance(properties, list), 'missing SBOM identity')
    ids = [x.get('value') for x in properties if x.get('name') == 'aquasecurity:trivy:ImageID']
    diffs = [x.get('value') for x in properties if x.get('name') == 'aquasecurity:trivy:DiffID']
    require(ids == [image['image_id']] and sorted(diffs) == sorted(image['diff_ids']),
            'SBOM image identity mismatch')
    components = sbom.get('components')
    require(isinstance(components, list) and components, 'missing SBOM components')
    sbom_packages = {x['purl'] for x in components if isinstance(x, dict) and x.get('purl')}
    require(packages == sbom_packages, 'report and SBOM package inventory disagree')
    return {'packages': len(packages), 'findings': findings,
            'blocking': [x for x in findings if x['severity'] in ('UNKNOWN', 'HIGH', 'CRITICAL')]}


def validate_directory(directory, now=None):
    directory = Path(directory)
    now = now or dt.datetime.now(dt.timezone.utc)
    receipt = read_json(directory / 'manifest.json')
    require(isinstance(receipt, dict) and receipt.get('format') == 'gm-vector-image-scan-v1',
            'invalid scan manifest')
    require(isinstance(receipt.get('source_revision'), str) and REVISION.fullmatch(receipt['source_revision']),
            'missing source revision')
    require(receipt.get('scanner') == {'image': SCANNER, 'version': VERSION}, 'unpinned scanner')
    require(isinstance(receipt.get('candidate_release'), dict)
            and receipt['candidate_release'].get('git_commit') == receipt['source_revision'],
            'candidate binary source revision mismatch')
    require(receipt.get('approval_mode') == 'disabled' and receipt.get('exceptions') == [],
            'D14 exception admission is disabled')
    started, finished = timestamp(receipt.get('started_at')), timestamp(receipt.get('finished_at'))
    require(started <= finished <= now, 'invalid scan window')
    database = read_json(directory / 'db-metadata.json')
    db_updated = validate_db(database, now)
    require(receipt.get('db_metadata_sha256') == file_hash(directory / 'db-metadata.json'),
            'advisory metadata hash mismatch')
    require(receipt.get('db_updated_at') == database.get('UpdatedAt'), 'advisory epoch mismatch')
    require(receipt.get('db_sha256') == file_hash(directory / 'cache' / 'db' / 'trivy.db'),
            'advisory database content mismatch')
    images = receipt.get('images')
    require(isinstance(images, dict) and set(images) == set(LABELS), 'three-image membership incomplete')
    summaries = {}
    for label in LABELS:
        image = images[label]
        require(isinstance(image, dict), 'malformed image identity')
        for suffix, key in (('image.tar', 'archive_sha256'), ('vulns.json', 'report_sha256'),
                            ('sbom.json', 'sbom_sha256')):
            require(file_hash(directory / (label + '-' + suffix)) == image.get(key), 'evidence hash mismatch')
        inventory = archive_inventory(directory / (label + '-image.tar'), image.get('image_id'), image.get('diff_ids'))
        require(inventory == image.get('archive_inventory'), 'saved archive inventory mismatch')
        summaries[label] = validate_reports(label, image, read_json(directory / (label + '-vulns.json')),
                                           read_json(directory / (label + '-sbom.json')), started, finished, db_updated)
    return {'format': 'gm-vector-image-gate-v1', 'source_revision': receipt['source_revision'],
            'status': 'BLOCKED' if any(x['blocking'] for x in summaries.values()) else 'PASS',
            'db_updated_at': database['UpdatedAt'], 'images': summaries}
