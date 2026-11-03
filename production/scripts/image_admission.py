"""D14 protected-review binding. Raw evidence comes from trusted producer code.

Downloaded data never supplies executable code or its own authorization context.
The pinned download action must verify the immutable artifact ZIP digest before
calling this verifier; this module rehashes all actual canonical member bytes.
"""
import datetime as dt
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import selectors
import stat
import struct
import subprocess
import sys
import time
import unicodedata
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

import image_gate as gate

REPOSITORY = 'DrToppi/gm-vector'
REPOSITORY_ID = '1438943020'
OWNER_ID = '161593815'
ENVIRONMENT = 'security-exceptions'
WORKFLOW = '.github/workflows/production.yml'
CONTEXT_KEYS = {'repository', 'repository_id', 'run_id', 'run_attempt', 'candidate_sha',
                'verifier_sha', 'event_name', 'workflow_ref'}
HEX = re.compile(r'[0-9a-f]{64}\Z')
SHA = re.compile(r'[0-9a-f]{40}\Z')
DECIMAL = re.compile(r'[1-9][0-9]{0,19}\Z')
LABELS = ('app', 'proxy', 'postgres')
FILES = tuple(sorted(['manifest.json', 'waivers.json'] +
                     [role + suffix for role in LABELS for suffix in ('.report.json', '.inventory.json')]))
BUNDLE_LIMIT = 2 * 1024 * 1024
REQUEST_LIMIT = 64 * 1024
ROWS_LIMIT = 20000
BUILD_KEYS = {'binary_sha256', 'web_inventory_sha256', 'context_archive_sha256',
              'source_archive_sha256', 'image_assets_sha256'}
REQUEST_KEYS = {'role', 'advisory_id', 'package', 'version', 'target', 'reason',
                'compensating_controls', 'expires_at', 'recheck_at'}
SECRET = re.compile(r'(?i)(?:gh[pousr]_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,}|AKIA[0-9A-Z]{16}|'
                    r'eyJ[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}|'
                    r'-----BEGIN[^\n]{0,30}PRIVATE KEY-----|\bbearer\s+\S+|'
                    r'\b(?:password|passwd|api[_ -]?key|secret|access[_ -]?token)\s*[:=]\s*\S+)')
EMAIL = re.compile(r'[\w.+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}(?:\b|$)')
PRIVATE_PATH = re.compile(r'(?i)(?:/home/|/root/|/Users/|[a-z]:\\Users\\|/run/secrets|/tmp/|file://|'
                          r'\b(?:localhost|[a-z0-9.-]+\.(?:internal|corp|lan|local))\b)')


class AdmissionError(ValueError):
    """Only static messages are public; never format an untrusted input value."""


def require(condition, message):
    if not condition:
        raise AdmissionError(message)


def keys(value, expected):
    require(type(value) is dict and set(value) == set(expected), 'unexpected or missing object fields')
    return value


def canonical(value):
    try:
        return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':'),
                          allow_nan=False).encode('utf-8')
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise AdmissionError('value cannot be represented canonically') from None


def digest(value):
    return hashlib.sha256(value).hexdigest()


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'duplicate JSON field')
        result[key] = value
    return result


def parse(raw, limit=BUNDLE_LIMIT):
    require(type(raw) is bytes and 0 < len(raw) <= limit, 'empty or oversized JSON')
    try:
        return json.loads(raw, object_pairs_hook=unique,
                          parse_constant=lambda _: (_ for _ in ()).throw(AdmissionError('invalid JSON number')))
    except (ValueError, UnicodeError, RecursionError):
        raise AdmissionError('malformed or duplicate JSON') from None


def regular_directory(path):
    path = Path(path).absolute()
    require('..' not in path.parts and not any(p.is_symlink() for p in (path, *path.parents)),
            'symlink directory refused')
    require(path.is_dir(), 'missing evidence directory')
    return path


def read_bytes(path, limit=BUNDLE_LIMIT):
    path = Path(path).absolute()
    regular_directory(path.parent)
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= limit, 'invalid evidence file')
            raw = stream.read(limit + 1)
        require(0 < len(raw) <= limit, 'oversized evidence file')
        return raw
    except OSError:
        raise AdmissionError('evidence file unavailable') from None


def write_file(path, value):
    raw = canonical(value) + b'\n'
    require(len(raw) <= BUNDLE_LIMIT, 'public evidence exceeds budget')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())


def public_string(value, *, maximum=1024, nullable=False, rationale=False):
    if value is None and nullable:
        return None
    require(type(value) is str, 'invalid public string')
    try:
        length = len(value.encode('utf-8'))
    except UnicodeError:
        raise AdmissionError('unsafe public string') from None
    require(length <= maximum and (not rationale or value.strip()), 'empty or oversized public text')
    require(not any(unicodedata.category(c) in ('Cc', 'Cf', 'Cs') and not (rationale and c == '\n') for c in value),
            'unsafe control character in public text')
    require(not SECRET.search(value) and not EMAIL.search(value) and not PRIVATE_PATH.search(value),
            'sensitive content refused in public projection')
    if rationale:
        candidates = re.findall(r'(?<![0-9.])(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?![0-9.])', value)
        candidates += re.findall(r'(?:[0-9a-fA-F]{0,4}:){2,}[0-9a-fA-F:.]+', value)
        for candidate in candidates:
            try:
                address = ipaddress.ip_address(candidate)
            except ValueError:
                continue
            require(not (address.is_private or address.is_loopback or address.is_link_local),
                    'private address refused in public text')
    return value


def utc(raw):
    require(type(raw) is str and len(raw) <= 64, 'missing UTC timestamp')
    try:
        result = dt.datetime.fromisoformat(raw.replace('Z', '+00:00'))
        require(result.tzinfo is not None and result.utcoffset() == dt.timedelta(0), 'timestamp must be UTC')
        return result.astimezone(dt.timezone.utc)
    except (ValueError, OverflowError):
        raise AdmissionError('malformed UTC timestamp') from None


def time_string(value):
    require(value.tzinfo is not None and value.utcoffset() == dt.timedelta(0), 'clock must be UTC')
    return value.isoformat().replace('+00:00', 'Z')


def decimal(value):
    require(type(value) is str and DECIMAL.fullmatch(value), 'invalid numeric identity')
    return value


def api_decimal(value):
    require(type(value) in (int, str) and type(value) is not bool, 'invalid API numeric identity')
    return decimal(str(value))


def sha(value):
    require(type(value) is str and HEX.fullmatch(value), 'invalid SHA-256 reference')
    return value


def artifact_digest(value):
    require(type(value) is str, 'invalid artifact digest')
    normalized = value if value.startswith('sha256:') else 'sha256:' + value
    require(gate.DIGEST.fullmatch(normalized), 'invalid artifact digest')
    return normalized


def validate_context(value):
    keys(value, CONTEXT_KEYS)
    require(value['repository'] == REPOSITORY and value['repository_id'] == REPOSITORY_ID,
            'unexpected repository context')
    for name in ('run_id', 'run_attempt'):
        decimal(value[name])
    require(type(value['candidate_sha']) is str and SHA.fullmatch(value['candidate_sha'])
            and type(value['verifier_sha']) is str and SHA.fullmatch(value['verifier_sha']), 'invalid source revision')
    require(value['event_name'] in ('push', 'pull_request', 'workflow_dispatch'), 'unsupported workflow event')
    public_string(value['workflow_ref'])
    require(value['workflow_ref'].startswith(REPOSITORY + '/' + WORKFLOW + '@refs/'), 'unexpected verifier workflow')
    return dict(value)


def runner_context(env=None):
    env = os.environ if env is None else env
    mapping = {'repository': 'GITHUB_REPOSITORY', 'repository_id': 'GITHUB_REPOSITORY_ID',
               'run_id': 'GITHUB_RUN_ID', 'run_attempt': 'GITHUB_RUN_ATTEMPT', 'candidate_sha': 'GITHUB_SHA',
               'verifier_sha': 'GITHUB_WORKFLOW_SHA', 'event_name': 'GITHUB_EVENT_NAME',
               'workflow_ref': 'GITHUB_WORKFLOW_REF'}
    return validate_context({name: env.get(variable) for name, variable in mapping.items()})


def validate_privacy(receipt, manifest, raw_manifest_hash):
    keys(receipt, {'format', 'status', 'source_revision', 'source_tree_clean', 'scanner',
                   'scan_manifest_sha256', 'image_ids', 'roots', 'build'})
    require(receipt['format'] == 'gm-vector-artifact-privacy-v1' and receipt['status'] == 'PASS'
            and receipt['source_tree_clean'] is True, 'complete privacy proof required')
    require(receipt['source_revision'] == manifest['source_revision']
            and receipt['scan_manifest_sha256'] == raw_manifest_hash, 'privacy scan/source binding mismatch')
    require(receipt['scanner'] == {'gitleaks_version': '8.30.1',
            'binary_sha256': '88f91962aa2f93ac6ab281d553b9e125f5197bbbce38f9f2437f7299c32e5509'}, 'unpinned privacy scanner')
    require(receipt['image_ids'] == {name: manifest['images'][name]['image_id'] for name in LABELS},
            'privacy image membership mismatch')
    roots = receipt['roots']
    require(type(roots) is list and len(roots) == 6, 'privacy root inventory incomplete')
    ids = []
    for item in roots:
        keys(item, {'id', 'status', 'members', 'body_bytes', 'inventory_sha256', 'content_sha256', 'scanner_report_sha256'})
        ids.append(item['id'])
        require(item['status'] == 'PASS' and type(item['members']) is int and item['members'] > 0
                and type(item['body_bytes']) is int and item['body_bytes'] >= 0, 'privacy root not verified')
        for key in ('inventory_sha256', 'content_sha256', 'scanner_report_sha256'):
            sha(item[key])
    require(sorted(ids) == ['P01', 'P02', 'P03', 'P04', 'P05', 'P06'], 'privacy root membership mismatch')
    keys(receipt['build'], BUILD_KEYS)
    for value in receipt['build'].values():
        sha(value)


def optional_text(row, name):
    return public_string(row.get(name), nullable=True)


def report_projection(role, image_id, report):
    results = report['Results']
    require(0 < len(results) <= 16, 'report group budget exceeded')
    projected, packages_count, findings_count = [], 0, 0
    for result in results:
        packages = []
        for item in result['Packages']:
            packages_count += 1
            require(packages_count <= ROWS_LIMIT, 'package row budget exceeded')
            epoch = item.get('Epoch')
            require(epoch is None or type(epoch) is int and epoch >= 0, 'invalid package epoch')
            packages.append({'id': optional_text(item, 'ID'), 'name': public_string(item['Name']),
                'version': optional_text(item, 'Version'), 'release': optional_text(item, 'Release'), 'epoch': epoch,
                'purl': public_string(item['Identifier']['PURL']), 'uid': optional_text(item['Identifier'], 'UID'),
                'bom_ref': optional_text(item['Identifier'], 'BOMRef')})
        vulnerabilities = []
        for item in result.get('Vulnerabilities', []):
            findings_count += 1
            require(findings_count <= ROWS_LIMIT, 'finding row budget exceeded')
            identifier = item.get('PkgIdentifier') or {}
            require(type(identifier) is dict, 'invalid finding package identifier')
            vulnerabilities.append({'id': public_string(item['VulnerabilityID']), 'package_id': optional_text(item, 'PkgID'),
                'package': public_string(item['PkgName']), 'package_path': optional_text(item, 'PkgPath'),
                'version': public_string(item['InstalledVersion']), 'fixed_version': optional_text(item, 'FixedVersion'),
                'severity': public_string(item['Severity']), 'status': optional_text(item, 'Status'),
                'package_purl': optional_text(identifier, 'PURL'), 'package_uid': optional_text(identifier, 'UID'),
                'package_bom_ref': optional_text(identifier, 'BOMRef')})
        projected.append({'target': public_string(result['Target']), 'class': public_string(result['Class']),
                          'type': public_string(result['Type']), 'packages': packages, 'vulnerabilities': vulnerabilities})
    return {'format': 'gm-vector-review-report-v1', 'role': role, 'image_id': image_id, 'results': projected}


def inventory_projection(role, image_id, sbom):
    count = [0]
    def component(item, depth=0, root=False):
        require(type(item) is dict and depth <= 16, 'invalid component inventory')
        count[0] += int(not root)
        require(count[0] <= ROWS_LIMIT, 'component row budget exceeded')
        children = item.get('components', [])
        require(type(children) is list, 'invalid nested component membership')
        return {'bom_ref': optional_text(item, 'bom-ref'), 'type': optional_text(item, 'type'), 'group': optional_text(item, 'group'),
                'name': optional_text(item, 'name'), 'version': optional_text(item, 'version'), 'purl': optional_text(item, 'purl'),
                'components': [component(child, depth + 1) for child in children]}
    dependencies = []
    source_dependencies = sbom.get('dependencies', [])
    require(type(source_dependencies) is list and len(source_dependencies) <= ROWS_LIMIT, 'dependency row budget exceeded')
    for item in source_dependencies:
        require(type(item) is dict and set(item) <= {'ref', 'dependsOn', 'provides'} and 'ref' in item,
                'unsupported dependency membership')
        output = {'ref': public_string(item['ref'])}
        for field in ('dependsOn', 'provides'):
            values = item.get(field, [])
            require(type(values) is list and len(values) <= ROWS_LIMIT, 'dependency edge budget exceeded')
            output[field] = [public_string(x) for x in values]
        dependencies.append(output)
    vulnerabilities = []
    source_vulnerabilities = sbom.get('vulnerabilities', [])
    require(type(source_vulnerabilities) is list and len(source_vulnerabilities) <= ROWS_LIMIT, 'inventory finding budget exceeded')
    for item in source_vulnerabilities:
        require(type(item) is dict, 'invalid inventory finding membership')
        affects = item.get('affects', [])
        require(type(affects) is list and len(affects) <= ROWS_LIMIT, 'inventory affects budget exceeded')
        projected = []
        for affected in affects:
            require(type(affected) is dict and set(affected) <= {'ref', 'versions'} and 'ref' in affected,
                    'unsupported affected component membership')
            versions = affected.get('versions', [])
            require(type(versions) is list and len(versions) <= ROWS_LIMIT, 'affected version budget exceeded')
            for version in versions:
                require(type(version) is dict and set(version) <= {'version', 'range', 'status'}, 'unsupported affected version')
            projected.append({'ref': public_string(affected['ref']), 'versions': [
                {key: optional_text(version, key) for key in ('version', 'range', 'status')} for version in versions]})
        vulnerabilities.append({'id': optional_text(item, 'id'), 'bom_ref': optional_text(item, 'bom-ref'), 'affects': projected})
    return {'format': 'gm-vector-review-inventory-v1', 'role': role, 'image_id': image_id,
            'root': component(sbom['metadata']['component'], root=True),
            'components': [component(item) for item in sbom['components']],
            'dependencies': dependencies, 'vulnerabilities': vulnerabilities}


def finding_rows(reports):
    result = []
    for role in LABELS:
        for group in reports[role]['results']:
            for row in group['vulnerabilities']:
                result.append({'role': role, 'image_id': reports[role]['image_id'], 'target': group['target'], **row})
    return result


def request_rows(request):
    if request is None:
        return []
    require(len(canonical(request)) <= REQUEST_LIMIT, 'exception request exceeds budget')
    keys(request, {'format', 'requests'})
    require(request['format'] == 'gm-vector-image-exception-request-v1' and type(request['requests']) is list
            and len(request['requests']) <= 100, 'invalid exception request')
    return request['requests']


def resolved_requests(request, reports, issued, now):
    rows = finding_rows(reports)
    require(not any(row['severity'] == 'UNKNOWN' for row in rows), 'UNKNOWN severity requires separate triage')
    blockers = [row for row in rows if row['severity'] in ('HIGH', 'CRITICAL')]
    resolved, used = [], set()
    for item in request_rows(request):
        keys(item, REQUEST_KEYS)
        require(item['role'] in LABELS, 'invalid image role')
        for name in ('advisory_id', 'package', 'version', 'target'):
            public_string(item[name])
            require(item[name] and not any(c in item[name] for c in '*?[]'), 'wildcard or empty waiver scope')
        public_string(item['reason'], maximum=2048, rationale=True)
        public_string(item['compensating_controls'], maximum=2048, rationale=True)
        expiry, recheck = utc(item['expires_at']), utc(item['recheck_at'])
        require(issued < recheck <= expiry <= issued + dt.timedelta(days=14) and now < recheck and now < expiry,
                'expired or overlong exception policy')
        scope = tuple(item[k] for k in ('role', 'advisory_id', 'package', 'version', 'target'))
        require(scope not in used, 'duplicate or overlapping exception request')
        matches = [row for row in blockers if (row['role'], row['id'], row['package'], row['version'], row['target']) == scope]
        require(matches, 'unused exception request')
        used.add(scope)
        resolved.append({**item, 'image_id': reports[item['role']]['image_id'], 'matched_count': len(matches),
                         'findings_sha256': digest(canonical(matches))})
    for row in blockers:
        require((row['role'], row['id'], row['package'], row['version'], row['target']) in used,
                'uncovered blocking finding')
    return {'format': 'gm-vector-resolved-waivers-v1', 'requests': resolved}


def prepare(scan_dir, privacy_receipt, output_dir, request=None, *, context, now=None):
    context = validate_context(context)
    now = now or dt.datetime.now(dt.timezone.utc)
    issued = utc(time_string(now))
    scan_dir = regular_directory(scan_dir)
    try:
        strict = gate.validate_directory(scan_dir, now)
        manifest_raw = read_bytes(scan_dir / 'manifest.json', gate.JSON_LIMIT)
        manifest = gate.json_bytes(manifest_raw)
        privacy_raw = read_bytes(privacy_receipt, BUNDLE_LIMIT)
        privacy = parse(privacy_raw)
        validate_privacy(privacy, manifest, digest(manifest_raw))
        require(manifest['source_revision'] == context['candidate_sha'], 'candidate source does not match runner')
        reports, inventories, identities, raw_refs = {}, {}, {}, {}
        for role in LABELS:
            image = manifest['images'][role]
            reports[role] = report_projection(role, image['image_id'], gate.read_json(scan_dir / (role + '-vulns.json')))
            inventories[role] = inventory_projection(role, image['image_id'], gate.read_json(scan_dir / (role + '-sbom.json')))
            registries = image.get('registry_manifests', [])
            require(type(registries) is list, 'invalid registry manifest inventory')
            registry_digests = []
            for reference in registries:
                require(type(reference) is str and '@' in reference, 'invalid registry manifest reference')
                registry_digests.append(artifact_digest(reference.rsplit('@', 1)[1]))
            identities[role] = {'image_id': image['image_id'], 'diff_ids': image['diff_ids'],
                                'registry_manifest_digests': registry_digests}
            raw_refs[role] = {name: sha(image[name]) for name in ('report_sha256', 'sbom_sha256', 'archive_sha256')}
        waivers = resolved_requests(request, reports, issued, now)
        if waivers['requests']:
            require(context['event_name'] == 'workflow_dispatch', 'exceptions require manual workflow dispatch')
        else:
            require(strict['status'] == 'PASS', 'strict scan remains blocked')
        payloads = {'waivers.json': waivers}
        for role in LABELS:
            payloads[role + '.report.json'] = reports[role]
            payloads[role + '.inventory.json'] = inventories[role]
        descriptors = [{'name': name, 'size': len(canonical(value)) + 1, 'sha256': digest(canonical(value) + b'\n')}
                       for name, value in sorted(payloads.items())]
        payloads['manifest.json'] = {'format': 'gm-vector-review-bundle-v1', 'context': context,
            'issued_at': time_string(issued), 'nonce': os.urandom(32).hex(), 'scanner': manifest['scanner'],
            'db': {'version': 2, 'updated_at': manifest['db_updated_at'],
                   'metadata_sha256': sha(manifest['db_metadata_sha256']), 'sha256': sha(manifest['db_sha256'])},
            'images': identities, 'provenance': {'scan_manifest_sha256': digest(manifest_raw),
                'privacy_receipt_sha256': digest(privacy_raw), 'raw_images': raw_refs, 'build': privacy['build']},
            'strict_status': strict['status'], 'files': descriptors}
        require(sum(len(canonical(x)) + 1 for x in payloads.values()) <= BUNDLE_LIMIT, 'review bundle exceeds budget')
        output_dir = Path(output_dir).absolute()
        regular_directory(output_dir.parent)
        output_dir.mkdir(mode=0o700, exist_ok=False)
        for name, value in sorted(payloads.items()):
            write_file(output_dir / name, value)
        bundle = load_bundle(output_dir, context=context, now=now)
        return {'status': 'REVIEW_REQUIRED' if waivers['requests'] else 'PASS', 'strict_status': strict['status'],
                'request_count': len(waivers['requests']), 'bundle_digest': bundle['bundle_digest']}
    except gate.GateError:
        raise AdmissionError('raw scan/privacy evidence validation failed') from None


def validate_projected_report(value, role, image_id):
    keys(value, {'format', 'role', 'image_id', 'results'})
    require(value['format'] == 'gm-vector-review-report-v1' and value['role'] == role
            and value['image_id'] == image_id, 'review report identity mismatch')
    groups = value['results']
    require(type(groups) is list and 0 < len(groups) <= 16, 'invalid review result inventory')
    packages, findings, targets = 0, 0, set()
    family = 'debian' if role == 'app' else 'alpine'
    classes = set()
    for group in groups:
        keys(group, {'target', 'class', 'type', 'packages', 'vulnerabilities'})
        target = tuple(public_string(group[k]) for k in ('target', 'class', 'type'))
        require(all(target) and target not in targets, 'ambiguous review target')
        targets.add(target); classes.add(target[1:])
        require(target[1:] in ({('os-pkgs', family)} | ({('lang-pkgs', 'gobinary')} if role == 'app' else set())),
                'unsupported review target')
        if target[1] == 'lang-pkgs':
            require(target[0].lstrip('/') == 'app/planner', 'unrelated Go target')
        require(type(group['packages']) is list and group['packages'], 'missing projected packages')
        require(type(group['vulnerabilities']) is list, 'invalid projected findings')
        packages += len(group['packages']); findings += len(group['vulnerabilities'])
        require(packages <= ROWS_LIMIT and findings <= ROWS_LIMIT, 'review row budget exceeded')
        for item in group['packages']:
            keys(item, {'id', 'name', 'version', 'release', 'epoch', 'purl', 'uid', 'bom_ref'})
            for key in ('id', 'version', 'release', 'uid', 'bom_ref'):
                public_string(item[key], nullable=True)
            require(public_string(item['name']) and public_string(item['purl']).startswith('pkg:'), 'invalid package identity')
            require(item['epoch'] is None or type(item['epoch']) is int and item['epoch'] >= 0, 'invalid package epoch')
        for item in group['vulnerabilities']:
            keys(item, {'id', 'package_id', 'package', 'package_path', 'version', 'fixed_version', 'severity', 'status',
                        'package_purl', 'package_uid', 'package_bom_ref'})
            for key in ('package_id', 'package_path', 'fixed_version', 'status', 'package_purl', 'package_uid', 'package_bom_ref'):
                public_string(item[key], nullable=True)
            require(all(public_string(item[key]) for key in ('id', 'package', 'version')), 'incomplete finding identity')
            require(item['severity'] in gate.SEVERITIES, 'invalid projected severity')
    require(('os-pkgs', family) in classes and (role != 'app' or ('lang-pkgs', 'gobinary') in classes),
            'review scan class omitted')


def validate_projected_inventory(value, role, image_id):
    keys(value, {'format', 'role', 'image_id', 'root', 'components', 'dependencies', 'vulnerabilities'})
    require(value['format'] == 'gm-vector-review-inventory-v1' and value['role'] == role
            and value['image_id'] == image_id, 'review inventory identity mismatch')
    # Convert only the projected field names back into the input shape, then
    # require the strict projection to reproduce every row, order and null.
    def component(item, depth=0):
        require(depth <= 16, 'component depth exceeded')
        keys(item, {'bom_ref', 'type', 'name', 'group', 'version', 'purl', 'components'})
        require(type(item['components']) is list, 'invalid nested components')
        return {'bom-ref': item['bom_ref'], **{k: item[k] for k in ('type', 'name', 'group', 'version', 'purl')},
                'components': [component(child, depth + 1) for child in item['components']]}
    require(type(value['components']) is list and type(value['dependencies']) is list
            and type(value['vulnerabilities']) is list, 'invalid inventory lists')
    for dependency in value['dependencies']:
        keys(dependency, {'ref', 'dependsOn', 'provides'})
    vulnerabilities = []
    for item in value['vulnerabilities']:
        keys(item, {'id', 'bom_ref', 'affects'})
        require(type(item['affects']) is list, 'invalid affected membership')
        for affected in item['affects']:
            keys(affected, {'ref', 'versions'})
            require(type(affected['versions']) is list, 'invalid affected versions')
            for version in affected['versions']:
                keys(version, {'version', 'range', 'status'})
        vulnerabilities.append({'id': item['id'], 'bom-ref': item['bom_ref'], 'affects': item['affects']})
    restored = inventory_projection(role, image_id, {'metadata': {'component': component(value['root'])},
        'components': [component(x) for x in value['components']], 'dependencies': value['dependencies'],
        'vulnerabilities': vulnerabilities})
    require(restored == value and value['root']['type'] == 'container', 'incomplete inventory projection')


def load_bundle(bundle_dir, *, context, now=None):
    context = validate_context(context)
    now = now or dt.datetime.now(dt.timezone.utc)
    directory = regular_directory(bundle_dir)
    names = []
    with os.scandir(directory) as entries:
        for entry in entries:
            require(len(names) < 8 and entry.is_file(follow_symlinks=False), 'extra or nonregular review member')
            names.append(entry.name)
    require(sorted(names) == list(FILES), 'review bundle membership mismatch')
    payloads, descriptors, total = {}, [], 0
    for name in FILES:
        raw = read_bytes(directory / name)
        total += len(raw)
        require(total <= BUNDLE_LIMIT, 'review bundle exceeds budget')
        value = parse(raw)
        require(raw == canonical(value) + b'\n', 'review member is not canonical')
        payloads[name] = value
        descriptors.append({'name': name, 'size': len(raw), 'sha256': digest(raw)})
    manifest = payloads['manifest.json']
    keys(manifest, {'format', 'context', 'issued_at', 'nonce', 'scanner', 'db', 'images', 'provenance', 'strict_status', 'files'})
    require(manifest['format'] == 'gm-vector-review-bundle-v1' and manifest['context'] == context,
            'review context mismatch')
    validate_context(manifest['context'])
    issued = utc(manifest['issued_at'])
    require(issued <= now, 'future review issuance')
    sha(manifest['nonce'])
    require(manifest['scanner'] == {'image': gate.SCANNER, 'version': gate.VERSION}, 'review scanner is not pinned')
    keys(manifest['db'], {'version', 'updated_at', 'metadata_sha256', 'sha256'})
    require(type(manifest['db']['version']) is int and manifest['db']['version'] == 2, 'unsupported advisory database')
    updated = utc(manifest['db']['updated_at'])
    require(updated <= issued and 0 <= (now - updated).total_seconds() <= 86400, 'review advisory database is stale or future')
    sha(manifest['db']['metadata_sha256']); sha(manifest['db']['sha256'])
    keys(manifest['images'], LABELS)
    for identity in manifest['images'].values():
        keys(identity, {'image_id', 'diff_ids', 'registry_manifest_digests'})
        artifact_digest(identity['image_id'])
        require(identity['image_id'].startswith('sha256:') and type(identity['diff_ids']) is list
                and 0 < len(identity['diff_ids']) <= 256 and type(identity['registry_manifest_digests']) is list,
                'invalid projected image identity')
        for value in identity['diff_ids'] + identity['registry_manifest_digests']:
            require(type(value) is str and gate.DIGEST.fullmatch(value), 'invalid projected layer identity')
    provenance = manifest['provenance']
    keys(provenance, {'scan_manifest_sha256', 'privacy_receipt_sha256', 'raw_images', 'build'})
    sha(provenance['scan_manifest_sha256']); sha(provenance['privacy_receipt_sha256'])
    keys(provenance['build'], BUILD_KEYS); keys(provenance['raw_images'], LABELS)
    for value in provenance['build'].values(): sha(value)
    for image in provenance['raw_images'].values():
        keys(image, {'report_sha256', 'sbom_sha256', 'archive_sha256'})
        for value in image.values(): sha(value)
    require(manifest['files'] == [x for x in descriptors if x['name'] != 'manifest.json'], 'review member hashes mismatch')
    reports, inventories = {}, {}
    for role in LABELS:
        image_id = manifest['images'][role]['image_id']
        reports[role] = payloads[role + '.report.json']; inventories[role] = payloads[role + '.inventory.json']
        validate_projected_report(reports[role], role, image_id)
        validate_projected_inventory(inventories[role], role, image_id)
    actual_status = 'BLOCKED' if any(x['severity'] in ('UNKNOWN', 'HIGH', 'CRITICAL') for x in finding_rows(reports)) else 'PASS'
    require(manifest['strict_status'] == actual_status, 'raw strict finding state was relabeled')
    waivers = payloads['waivers.json']
    keys(waivers, {'format', 'requests'})
    require(waivers['format'] == 'gm-vector-resolved-waivers-v1' and type(waivers['requests']) is list
            and len(waivers['requests']) <= 100, 'invalid resolved waiver inventory')
    requested = []
    for item in waivers['requests']:
        keys(item, REQUEST_KEYS | {'image_id', 'matched_count', 'findings_sha256'})
        require(type(item['matched_count']) is int and item['matched_count'] > 0, 'invalid matched finding count')
        sha(item['findings_sha256'])
        requested.append({key: item[key] for key in REQUEST_KEYS})
    recomputed = resolved_requests({'format': 'gm-vector-image-exception-request-v1', 'requests': requested}, reports, issued, now)
    require(waivers == recomputed, 'resolved scope or finding membership mismatch')
    if requested:
        require(context['event_name'] == 'workflow_dispatch', 'nonempty exceptions require manual workflow dispatch')
    else:
        require(actual_status == 'PASS', 'strict zero-request bundle blocked')
    return {'manifest': manifest, 'reports': reports, 'inventories': inventories, 'waivers': waivers,
            'bundle_digest': digest(b'gm-vector-review-bundle-v1\0' + canonical(descriptors))}


def environment_settings(environment, branch_policies=()):
    require(type(environment) is dict and environment.get('name') == ENVIRONMENT, 'unexpected protected environment')
    environment_id = api_decimal(environment.get('id'))
    require(environment.get('can_admins_bypass') is False, 'administrator bypass is not verifiably disabled')
    rules = environment.get('protection_rules')
    require(type(rules) is list and len(rules) <= 3, 'invalid environment protection rules')
    reviewers, prevent, wait, seen = None, None, 0, set()
    for rule in rules:
        require(type(rule) is dict and rule.get('type') in ('required_reviewers', 'wait_timer', 'branch_policy')
                and rule['type'] not in seen, 'unsupported or duplicate environment rule')
        seen.add(rule['type'])
        if rule['type'] == 'required_reviewers':
            require(rule.get('prevent_self_review') is False and type(rule.get('reviewers')) is list
                    and len(rule['reviewers']) == 1, 'sole-owner review policy mismatch')
            reviewer = rule['reviewers'][0]
            require(type(reviewer) is dict and reviewer.get('type') == 'User'
                    and type(reviewer.get('reviewer')) is dict
                    and api_decimal(reviewer['reviewer'].get('id')) == OWNER_ID, 'required owner reviewer missing')
            reviewers, prevent = OWNER_ID, False
        if rule['type'] == 'wait_timer':
            wait = rule.get('wait_timer')
            require(type(wait) is int and 0 <= wait <= 43200, 'invalid environment wait timer')
    require(reviewers == OWNER_ID, 'required owner review rule missing')
    branch = environment.get('deployment_branch_policy')
    if branch is not None:
        keys(branch, {'protected_branches', 'custom_branch_policies'})
        require(type(branch['protected_branches']) is bool and type(branch['custom_branch_policies']) is bool
                and not (branch['protected_branches'] and branch['custom_branch_policies']), 'invalid branch policy flags')
    require(type(branch_policies) in (list, tuple) and len(branch_policies) <= 1000, 'branch policy inventory exceeded')
    custom = []
    for item in branch_policies:
        require(type(item) is dict and item.get('type') in ('branch', 'tag'), 'invalid custom branch policy')
        custom.append({'id': api_decimal(item.get('id')), 'name': public_string(item.get('name')), 'type': item['type']})
    require(len({x['id'] for x in custom}) == len(custom), 'duplicate custom branch policy')
    require((branch is not None and branch['custom_branch_policies']) or not custom, 'unexpected custom branch rules')
    if branch is not None and branch['custom_branch_policies']:
        require(custom, 'custom branch policy inventory missing')
    return {'environment_id': environment_id, 'environment_name': ENVIRONMENT, 'reviewer_id': OWNER_ID,
            'prevent_self_review': prevent, 'wait_timer': wait, 'deployment_branch_policy': branch,
            'branch_policies': sorted(custom, key=lambda x: (x['id'], x['type'], x['name'])), 'can_admins_bypass': False}


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise AdmissionError('GitHub API redirect refused')


def _bounded_get(path, token, *, timeout=10, worker_factory=None):
    """A separate owned process bounds DNS/connect/body time, not just sockets.

    worker_factory and timeout are private unit injection points, never CLI or
    environment fault controls. Token transport is exclusively an environment.
    """
    command = worker_factory(path) if worker_factory else [sys.executable, str(Path(__file__).resolve()), '_github_get', path]
    deadline = time.monotonic() + timeout
    child = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             env={'GITHUB_TOKEN': token, 'PYTHONNOUSERSITE': '1', 'LANG': 'C.UTF-8'})
    selector = selectors.DefaultSelector()
    output = bytearray()
    try:
        for stream in (child.stdout, child.stderr):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ)
        while selector.get_map():
            remaining = deadline - time.monotonic()
            require(remaining > 0, 'GitHub read request deadline exceeded')
            for key, _ in selector.select(remaining):
                data = os.read(key.fileobj.fileno(), 65536)
                if not data:
                    selector.unregister(key.fileobj)
                    continue
                require(key.fileobj is child.stdout, 'GitHub worker diagnostic output refused')
                require(len(output) + len(data) <= BUNDLE_LIMIT + 16384 + 4, 'GitHub worker output exceeds budget')
                output.extend(data)
        remaining = deadline - time.monotonic()
        require(remaining > 0, 'GitHub read request deadline exceeded')
        try:
            code = child.wait(timeout=remaining)
        except subprocess.TimeoutExpired:
            raise AdmissionError('GitHub read request deadline exceeded') from None
        require(code == 0 and len(output) >= 4, 'GitHub read request failed')
        length = struct.unpack('>I', output[:4])[0]
        require(length <= 16384 and 4 + length < len(output), 'invalid GitHub worker protocol')
        raw = bytes(output[4 + length:])
        require(len(raw) <= BUNDLE_LIMIT, 'GitHub response exceeds budget')
        try:
            link = bytes(output[4:4 + length]).decode('utf-8')
        except UnicodeError:
            raise AdmissionError('invalid GitHub worker protocol') from None
        return raw, link
    finally:
        selector.close()
        if child.poll() is None:
            child.kill()
        child.wait(timeout=2)
        child.stdout.close(); child.stderr.close()


class GitHubAPI:
    """Only read endpoints, fixed origin, bounded bodies/pages and TLS defaults."""
    def __init__(self, *, env=None, opener=None):
        token = (os.environ if env is None else env).get('GITHUB_TOKEN')
        require(type(token) is str and 0 < len(token) <= 4096 and '\r' not in token and '\n' not in token,
                'read-only GitHub credential unavailable')
        self._token = token
        self._opener = opener

    def _page(self, path):
        require(type(path) is str and 0 < len(path) <= 2048 and path.isascii()
                and not any(c.isspace() or unicodedata.category(c) == 'Cc' for c in path), 'unsafe API endpoint')
        prefix = '/repos/' + REPOSITORY
        allowed = (r'/actions/runs/[1-9][0-9]*(?:/approvals|/artifacts)?',
                   r'/actions/artifacts/[1-9][0-9]*',
                   r'/environments/security-exceptions(?:/deployment-branch-policies)?')
        parts = urlsplit(path)
        require(not parts.scheme and not parts.netloc and not parts.fragment and
                (parts.path == prefix or any(re.fullmatch(re.escape(prefix) + value, parts.path) for value in allowed)),
                'unsupported GitHub read endpoint')
        try:
            query = parse_qs(parts.query, strict_parsing=True) if parts.query else {}
        except ValueError:
            raise AdmissionError('unsupported API pagination') from None
        require(not query or (set(query) == {'per_page', 'page'} and query['per_page'] == ['100']
                and len(query['page']) == 1 and query['page'][0] in [str(x) for x in range(1, 11)]),
                'unsupported API pagination')
        if self._opener is None:
            raw, link = _bounded_get(path, self._token)
            return parse(raw), link
        request = Request('https://api.github.com' + path, method='GET', headers={
            'Accept': 'application/vnd.github+json', 'Authorization': 'Bearer ' + self._token,
            'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'gm-vector-image-admission'})
        try:
            with self._opener.open(request, timeout=10) as response:
                require(response.status == 200, 'GitHub read request failed')
                raw = response.read(BUNDLE_LIMIT + 1)
                link = response.headers.get('Link', '')
            require(type(link) is str and len(link.encode('utf-8')) <= 16384, 'API header exceeds budget')
            return parse(raw), link
        except (OSError, HTTPError, URLError, ValueError):
            raise AdmissionError('GitHub read request failed') from None

    def get(self, path):
        value, link = self._page(path)
        require(not link, 'unexpected paginated API object')
        return value

    def pages(self, path, collection=None):
        result = []
        for page in range(1, 11):
            value, link = self._page(path + '?per_page=100&page=' + str(page))
            if collection is not None:
                require(type(value) is dict and type(value.get('total_count')) is int
                        and 0 <= value['total_count'] <= 1000 and collection in value, 'invalid paginated inventory')
                total = value['total_count']
                value = value[collection]
            else:
                total = None
            require(type(value) is list and len(value) <= 100, 'API page exceeds budget')
            result.extend(value)
            require(len(result) <= 1000, 'API pagination exceeds budget')
            next_links = re.findall(r'<([^>]+)>;\s*rel="next"', link)
            require(len(next_links) <= 1 and len(link) <= 16384, 'invalid API pagination link')
            if next_links:
                expected = 'https://api.github.com' + path + '?per_page=100&page=' + str(page + 1)
                actual = urlsplit(next_links[0])
                expected_parts = urlsplit(expected)
                numeric_path = '/repositories/' + REPOSITORY_ID + expected_parts.path[len('/repos/' + REPOSITORY):]
                require(actual.scheme == 'https' and actual.netloc == 'api.github.com'
                        and actual.path in (expected_parts.path, numeric_path) and not actual.fragment
                        and parse_qs(actual.query) == parse_qs(expected_parts.query), 'foreign API pagination link')
            # GitHub authenticates completion by absence of rel=next, including
            # an exactly full last page or a non-paginated approvals endpoint.
            done = not next_links
            if done:
                require(total is None or total == len(result), 'truncated API inventory')
                return result
        raise AdmissionError('API pagination exceeds budget')


def live_run(api, ctx, *, exceptions):
    ctx = validate_context(ctx)
    repository = api.get('/repos/' + REPOSITORY)
    require(type(repository) is dict and api_decimal(repository.get('id')) == REPOSITORY_ID
            and type(repository.get('owner')) is dict
            and api_decimal(repository['owner'].get('id')) == OWNER_ID
            and type(repository.get('full_name')) is str and repository['full_name'].lower() == REPOSITORY.lower(),
            'GitHub repository identity mismatch')
    run = api.get('/repos/' + REPOSITORY + '/actions/runs/' + ctx['run_id'])
    require(type(run) is dict and api_decimal(run.get('id')) == ctx['run_id']
            and api_decimal(run.get('run_attempt')) == ctx['run_attempt']
            and run.get('event') == ctx['event_name'], 'current run/attempt mismatch')
    require(type(run.get('repository')) is dict and api_decimal(run['repository'].get('id')) == REPOSITORY_ID
            and type(run.get('head_repository')) is dict and type(run.get('head_sha')) is str
            and SHA.fullmatch(run['head_sha']), 'run repository/source identity missing')
    head_repository = api_decimal(run['head_repository'].get('id'))
    require(type(run.get('path')) is str and run['path'].split('@')[0] == WORKFLOW, 'foreign workflow run')
    if exceptions:
        require(ctx['event_name'] == 'workflow_dispatch' and head_repository == REPOSITORY_ID
                and run['head_sha'] == ctx['candidate_sha'], 'exceptions require exact own-repository dispatch source')
        require(type(run.get('head_branch')) is str and ctx['workflow_ref'].rsplit('@', 1)[1]
                in ('refs/heads/' + run['head_branch'], 'refs/tags/' + run['head_branch']), 'verifier workflow ref mismatch')
    elif ctx['event_name'] != 'pull_request':
        require(head_repository == REPOSITORY_ID and run['head_sha'] == ctx['candidate_sha'], 'strict run source mismatch')
    # PR strict-only context deliberately retains GITHUB_SHA's merge candidate;
    # REST run/artifact head_sha is checked against the live PR head, not guessed.
    return run


def verify_artifact(api, artifact_id, supplied_digest, ctx, run, issued, now):
    decimal(artifact_id)
    supplied_digest = artifact_digest(supplied_digest)
    value = api.get('/repos/' + REPOSITORY + '/actions/artifacts/' + artifact_id)
    require(type(value) is dict and api_decimal(value.get('id')) == artifact_id
            and value.get('digest') == supplied_digest and value.get('expired') is False,
            'immutable artifact identity/digest mismatch')
    require(type(value.get('size_in_bytes')) is int and 0 < value['size_in_bytes'] <= BUNDLE_LIMIT + 16384,
            'artifact size exceeds budget')
    created, expires = utc(value.get('created_at')), utc(value.get('expires_at'))
    require(issued.replace(microsecond=0) <= created <= now and expires > now, 'expired or stale artifact metadata')
    workflow = value.get('workflow_run')
    require(type(workflow) is dict and api_decimal(workflow.get('id')) == ctx['run_id']
            and api_decimal(workflow.get('repository_id')) == REPOSITORY_ID
            and api_decimal(workflow.get('head_repository_id')) == api_decimal(run['head_repository'].get('id'))
            and workflow.get('head_sha') == run['head_sha'], 'artifact belongs to another run/repository/source')
    listing = api.pages('/repos/' + REPOSITORY + '/actions/runs/' + ctx['run_id'] + '/artifacts', collection='artifacts')
    require(type(listing) is list and len(listing) <= 1000, 'artifact inventory exceeds budget')
    require(all(type(item) is dict for item in listing), 'malformed artifact inventory')
    matches = [item for item in listing if api_decimal(item.get('id')) == artifact_id]
    require(len(matches) == 1 and matches[0].get('digest') == supplied_digest
            and matches[0].get('expired') is False, 'artifact run membership missing or ambiguous')
    return {'id': artifact_id, 'digest': supplied_digest}


def binding(bundle, artifact, ctx):
    return {**ctx, 'nonce': bundle['manifest']['nonce'], 'bundle_digest': bundle['bundle_digest'],
            'artifact_id': artifact['id'], 'artifact_digest': artifact['digest']}


def bind(bundle_dir, artifact_id, artifact_digest, *, context, api, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    bundle = load_bundle(bundle_dir, context=context, now=now)
    exceptions = bool(bundle['waivers']['requests'])
    run = live_run(api, context, exceptions=exceptions)
    artifact = verify_artifact(api, artifact_id, artifact_digest, context, run, utc(bundle['manifest']['issued_at']), now)
    value = digest(b'gm-vector-approval-v1\0' + canonical(binding(bundle, artifact, context)))
    return {'status': 'REVIEW_REQUIRED' if exceptions else 'PASS', 'strict_status': bundle['manifest']['strict_status'],
            'request_count': len(bundle['waivers']['requests']), 'bundle_digest': bundle['bundle_digest'],
            'artifact_id': artifact['id'], 'artifact_digest': artifact['digest'], 'approval_digest': value,
            'review_comment': 'GM_VECTOR_EXCEPTION_APPROVAL_V1 ' + value}


def setup_proof(config, api, now):
    keys(config, {'format', 'status', 'repository_id', 'environment_id', 'environment_name', 'reviewer_id',
                  'api_settings_sha256', 'bypass_disable_verification'})
    require(config['format'] == 'gm-vector-exception-environment-v1' and config['status'] == 'verified'
            and config['repository_id'] == REPOSITORY_ID and config['reviewer_id'] == OWNER_ID
            and config['environment_name'] == ENVIRONMENT, 'protected environment setup remains blocked')
    decimal(config['environment_id']); sha(config['api_settings_sha256'])
    evidence = config['bypass_disable_verification']
    keys(evidence, {'method', 'verified_at', 'evidence_sha256'})
    require(evidence['method'] == 'api_readback' and utc(evidence['verified_at']) <= now,
            'administrator bypass readback proof missing')
    sha(evidence['evidence_sha256'])
    environment = api.get('/repos/' + REPOSITORY + '/environments/' + ENVIRONMENT)
    branch = environment.get('deployment_branch_policy') if type(environment) is dict else None
    policies = api.pages('/repos/' + REPOSITORY + '/environments/' + ENVIRONMENT + '/deployment-branch-policies',
                         collection='branch_policies') if type(branch) is dict and branch.get('custom_branch_policies') is True else []
    settings = environment_settings(environment, policies)
    require(settings['environment_id'] == config['environment_id']
            and digest(canonical(settings)) == config['api_settings_sha256'], 'live environment settings changed')
    return {'environment_id': settings['environment_id'], 'settings_sha256': config['api_settings_sha256'],
            'setup_sha256': digest(canonical(config))}


def owner_proof(api, ctx, environment_id, approval_digest):
    comment = 'GM_VECTOR_EXCEPTION_APPROVAL_V1 ' + sha(approval_digest)
    records = api.pages('/repos/' + REPOSITORY + '/actions/runs/' + ctx['run_id'] + '/approvals')
    require(type(records) is list and len(records) <= 1000, 'review inventory exceeds budget')
    approved = []
    for record in records:
        require(type(record) is dict and type(record.get('user')) is dict
                and type(record.get('environments')) is list, 'malformed owner review history')
        if api_decimal(record['user'].get('id')) != OWNER_ID or record.get('comment') != comment:
            continue
        environments = record['environments']
        matching = [item for item in environments if type(item) is dict and api_decimal(item.get('id')) == environment_id
                    and item.get('name') == ENVIRONMENT]
        if not matching:
            continue
        require(record.get('state') == 'approved' and len(matching) == 1, 'owner review rejected or ambiguous')
        approved.append({'state': 'approved', 'owner_id': OWNER_ID, 'environment_id': environment_id, 'comment': comment})
    require(len(approved) == 1, 'exact owner review proof missing or ambiguous')
    return digest(canonical(approved[0]))


def approve(bundle_dir, artifact_id, artifact_digest, environment_config, *, context, api, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    bundle = load_bundle(bundle_dir, context=context, now=now)
    require(bundle['waivers']['requests'], 'zero-request path must not enter protected admission')
    attached = bind(bundle_dir, artifact_id, artifact_digest, context=context, api=api, now=now)
    setup = setup_proof(environment_config, api, now)
    proof = owner_proof(api, context, setup['environment_id'], attached['approval_digest'])
    return {'format': 'gm-vector-image-admission-v1', 'status': 'ADMITTED_WITH_EXCEPTIONS',
            'context': context, 'strict_status': bundle['manifest']['strict_status'],
            'bundle_digest': bundle['bundle_digest'], 'nonce': bundle['manifest']['nonce'],
            'artifact_id': attached['artifact_id'], 'artifact_digest': attached['artifact_digest'],
            'approval_digest': attached['approval_digest'], 'owner_proof_sha256': proof,
            'waivers_sha256': digest(canonical(bundle['waivers'])), 'verified_at': time_string(now),
            'environment': setup}


def final_result(bundle_dir, artifact_id, artifact_digest, prepare_result, admission_result, *, context, api,
                 admission_file=None, admission_artifact_id=None, admission_artifact_digest=None,
                 environment_config=None, now=None):
    now = now or dt.datetime.now(dt.timezone.utc)
    require(prepare_result == 'success', 'required preparation did not succeed')
    bundle = load_bundle(bundle_dir, context=context, now=now)
    attached = bind(bundle_dir, artifact_id, artifact_digest, context=context, api=api, now=now)
    if not bundle['waivers']['requests']:
        require(admission_result == 'skipped' and all(value is None for value in
                (admission_file, admission_artifact_id, admission_artifact_digest, environment_config)),
                'strict zero-request path has unexpected admission result or receipt')
        return {'status': 'PASS', 'strict_status': 'PASS', 'context': context,
                'bundle_digest': bundle['bundle_digest'], 'artifact_id': attached['artifact_id'],
                'artifact_digest': attached['artifact_digest']}
    require(admission_result == 'success' and all(value is not None for value in
            (admission_file, admission_artifact_id, admission_artifact_digest, environment_config)),
            'required protected admission did not succeed')
    raw = read_bytes(admission_file, REQUEST_LIMIT)
    receipt = parse(raw, REQUEST_LIMIT)
    require(raw == canonical(receipt) + b'\n', 'admission receipt is not canonical')
    keys(receipt, {'format', 'status', 'context', 'strict_status', 'bundle_digest', 'nonce', 'artifact_id',
                   'artifact_digest', 'approval_digest', 'owner_proof_sha256', 'waivers_sha256', 'verified_at', 'environment'})
    verified = utc(receipt['verified_at'])
    require(utc(bundle['manifest']['issued_at']) <= verified <= now, 'future or stale admission receipt')
    expected = approve(bundle_dir, artifact_id, artifact_digest, environment_config, context=context, api=api, now=now)
    # Verification time may advance. All authorization/scope/proof fields must
    # still match live state; it is not an authenticated approval timestamp.
    expected['verified_at'] = receipt['verified_at']
    require(receipt == expected, 'admission receipt no longer matches current proof/scope')
    run = live_run(api, context, exceptions=True)
    admitted = verify_artifact(api, admission_artifact_id, admission_artifact_digest, context, run, verified, now)
    require(admitted['id'] != attached['artifact_id'], 'admission and review artifacts must differ')
    return {'status': 'ADMITTED_WITH_EXCEPTIONS', 'strict_status': bundle['manifest']['strict_status'],
            'context': context, 'bundle_digest': bundle['bundle_digest'], 'approval_digest': attached['approval_digest'],
            'admission_artifact_id': admitted['id'], 'admission_artifact_digest': admitted['digest']}


def _worker_main():
    require(len(sys.argv) == 3 and sys.argv[1] == '_github_get', 'invalid private worker invocation')
    api = GitHubAPI(opener=build_opener(ProxyHandler({}), NoRedirect()))
    value, link = api._page(sys.argv[2])
    raw, header = canonical(value), link.encode('utf-8')
    require(0 < len(raw) <= BUNDLE_LIMIT and len(header) <= 16384, 'API worker budget exceeded')
    sys.stdout.buffer.write(struct.pack('>I', len(header)) + header + raw)
    sys.stdout.buffer.flush()


if __name__ == '__main__':
    try:
        _worker_main()
    except BaseException:
        # No remote body, exception diagnostic or credential is an error channel.
        raise SystemExit(1)
