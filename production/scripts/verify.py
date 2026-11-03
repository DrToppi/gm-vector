#!/usr/bin/env python3
"""Run explicit verification suites without using production configuration."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
FOCUSED_HISTORY_TESTS = (
    'TestTimeHistoryLegacyMetadataRegression', 'TestTimeHistoryPaginationIntegration',
    'TestTimeHistoryRevisionAccessIntegration', 'TestTimeHistoryExportIntegration',
    'TestPortableProjectIntegration', 'TestPlanningTimezoneIntegration',
    'TestTimeHistoryRawQueryIntegration',
)
FOCUSED_INTEGRATION_TESTS = FOCUSED_HISTORY_TESTS + (
    'TestSecurityAuditMatrix', 'TestPlaywrightMemberMilestoneRegression',
)
PLAYWRIGHT_TEST = 'TestPlaywrightE2E'
PLAYWRIGHT_CHILDREN = tuple(PLAYWRIGHT_TEST + '/' + engine for engine in ('chromium', 'firefox', 'webkit'))


def require_go_test(stdout, expected_test, required_children=()):
    """An exit code alone also accepts missing or skipped Go tests."""
    terminal = []
    skipped_children = []
    children = {name: [] for name in required_children}
    for line in stdout.splitlines():
        event = json.loads(line)
        if 'Output' in event:
            print(event['Output'], end='', flush=True)
        if event.get('Test') == expected_test and event.get('Action') in ('pass', 'fail', 'skip'):
            terminal.append(event['Action'])
        if event.get('Test') in children and event.get('Action') in ('pass', 'fail', 'skip'):
            children[event['Test']].append(event['Action'])
        if event.get('Action') == 'skip' and str(event.get('Test', '')).startswith(expected_test + '/'):
            skipped_children.append(event['Test'])
    if terminal != ['pass'] or skipped_children or any(value != ['pass'] for value in children.values()):
        raise RuntimeError(f'{expected_test} must run and pass; missing/skipped/failed is not success')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('suite', choices=('unit', 'integration', 'e2e', 'all'))
    parser.add_argument('--dsn-file', type=Path,
                        help='explicit file with disposable gma_planner_test DSN; otherwise GMA_TEST_DSN')
    parser.add_argument('--only-test', choices=FOCUSED_INTEGRATION_TESTS,
                        help='run one named integration check; never a full-suite acceptance')
    parser.add_argument('--only-browser-api', action='store_true',
                        help='build web and run only real-API browser E2E; not full-suite acceptance')
    parser.add_argument('--only-playwright', action='store_true',
                        help='build web and run all three real Playwright engines; not complete E2E acceptance')
    parser.add_argument('--playwright-diagnostic-engine', choices=('chromium', 'firefox', 'webkit'),
                        help='single-engine diagnostic with --only-playwright; full-root guard remains enabled')
    parser.add_argument('--evidence-file', type=Path,
                        help='new private log file for all child output; existing files are refused')
    parser.add_argument('--nginx-bin', type=Path,
                        help='explicit native nginx binary for the real proxy regression; otherwise PATH/GMA_NGINX_BIN')
    for role in ('app', 'db', 'proxy'):
        parser.add_argument('--' + role + '-image', help='immutable image ID for complete integration/all recovery')
    parser.add_argument('--browser-library-dir', type=Path,
                        help='explicit isolated native library directory for local browser checks')
    parser.add_argument('--browser-debug-dir', type=Path,
                        help='new private directory outside source for failed-browser diagnostic logs only')
    args = parser.parse_args()
    if args.only_test and args.suite != 'integration':
        parser.error('--only-test requires the integration suite; all/unit/e2e stay complete')
    if args.only_browser_api and (args.suite != 'e2e' or args.only_test or args.only_playwright):
        parser.error('--only-browser-api requires e2e; all/unit/integration stay complete')
    if args.only_playwright and (args.suite != 'e2e' or args.only_test or args.only_browser_api):
        parser.error('--only-playwright requires e2e and cannot be combined with another focus')
    if args.playwright_diagnostic_engine and not args.only_playwright:
        parser.error('--playwright-diagnostic-engine requires explicit --only-playwright focus')
    recovery_images = {role: getattr(args, role + '_image') for role in ('app', 'db', 'proxy')}
    if any(recovery_images.values()) and (
            args.suite not in ('integration', 'all') or args.only_test
            or not all(value and re.fullmatch(r'sha256:[0-9a-f]{64}', value) for value in recovery_images.values())):
        parser.error('recovery image overrides require all three immutable IDs and a complete integration/all suite')
    if args.evidence_file:
        fd = os.open(args.evidence_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        try:
            os.dup2(fd, 1)
            os.dup2(fd, 2)
        finally:
            os.close(fd)
    env = os.environ.copy()
    for name in ('GMA_TEST_DSN', 'GMA_E2E', 'GMA_PERFORMANCE', 'GMA_PLAYWRIGHT_E2E', 'GMA_PW_DEBUG_DIRECTORY'):
        env.pop(name, None)
    env['CGO_ENABLED'] = '1'
    if args.nginx_bin:
        env['GMA_NGINX_BIN'] = str(args.nginx_bin.expanduser().resolve())
    if args.browser_library_dir:
        library_dir = args.browser_library_dir.expanduser().resolve()
        if args.suite not in ('e2e', 'all') or not library_dir.is_dir():
            parser.error('--browser-library-dir requires e2e/all and an existing directory')
        env['LD_LIBRARY_PATH'] = str(library_dir)
    if args.browser_debug_dir:
        debug_dir = args.browser_debug_dir.expanduser().resolve()
        if args.suite not in ('e2e', 'all') or debug_dir.is_relative_to(ROOT.parent) or debug_dir.exists():
            parser.error('--browser-debug-dir requires e2e/all and a new directory outside source')
        debug_dir.mkdir(mode=0o700)
        env['GMA_PW_DEBUG_DIRECTORY'] = str(debug_dir)
    go = os.environ.get('GMA_GO_BIN') or shutil.which('go')
    if not go:
        candidate = Path.home() / '.local/share/gma-toolchain/go/bin/go'
        if candidate.is_file():
            go = str(candidate)
    if not go:
        parser.error('Go is required (PATH or GMA_GO_BIN)')
    dsn = ''
    if args.suite != 'unit':
        try:
            dsn = (args.dsn_file.read_text().strip() if args.dsn_file
                   else os.environ.get('GMA_TEST_DSN', ''))
            parsed = urlsplit(dsn)
            valid = parsed.scheme in ('postgres', 'postgresql') and parsed.path == '/gma_planner_test'
        except (OSError, ValueError):
            valid = False
        if not valid:
            parser.error('provide a DSN for the isolated gma_planner_test database; production refused')

    def run(label, command, cwd=ROOT, extra=None, expected_test=None, required_children=()):
        print(f'Running: {label}', flush=True)
        result = subprocess.run(command, cwd=cwd, env={**env, **(extra or {})},
                                capture_output=bool(expected_test), text=True, timeout=600)
        if expected_test:
            if result.stderr:
                print(result.stderr, end='', file=sys.stderr)
            require_go_test(result.stdout, expected_test, required_children)
        result.check_returncode()
        print(f'PASS: {label}', flush=True)

    if args.only_test:
        print('Focused check only; this is not full integration acceptance.', flush=True)
        run(args.only_test + ' (race)',
            [go, 'test', '-race', '-count=1', '-json', '-run', '^' + args.only_test + '$', './internal/planner'],
            extra={'GMA_TEST_DSN': dsn}, expected_test=args.only_test)
        return 0

    if args.only_browser_api:
        print('Focused real-API browser check only; this is not full E2E acceptance.', flush=True)
        run('Frontend build for E2E', ['npm', 'run', 'build'], ROOT / 'web')
        run('Browser E2E (real API and PostgreSQL)',
            [go, 'test', '-race', '-count=1', '-json', '-timeout', '5m',
             '-run', '^TestBrowserE2E$', './internal/planner'],
            extra={'GMA_TEST_DSN': dsn, 'GMA_E2E': '1'}, expected_test='TestBrowserE2E')
        return 0

    def playwright():
        pattern = '^' + PLAYWRIGHT_TEST + '$'
        label = 'Three-engine E2E (real API and PostgreSQL)'
        if args.playwright_diagnostic_engine:
            pattern = '^' + PLAYWRIGHT_TEST + '$/^' + args.playwright_diagnostic_engine + '$'
            label = 'Single-engine diagnostic; full-root completeness guard stays enabled'
        run(label,
            [go, 'test', '-race', '-count=1', '-json', '-timeout', '10m',
             '-run', pattern, './internal/planner'],
            extra={'GMA_TEST_DSN': dsn, 'GMA_PLAYWRIGHT_E2E': '1'},
            expected_test=PLAYWRIGHT_TEST, required_children=PLAYWRIGHT_CHILDREN)

    if args.only_playwright:
        print('Focused three-engine check only; this is not complete E2E acceptance.', flush=True)
        if args.playwright_diagnostic_engine:
            print('Diagnostic filter only: cannot satisfy the full-root three-engine gate.', flush=True)
        run('Frontend build for E2E', ['npm', 'run', 'build'], ROOT / 'web')
        playwright()
        return 0

    if args.suite in ('unit', 'all'):
        run('Frontend typecheck', ['npm', 'run', 'check'], ROOT / 'web')
        run('Frontend test/config typecheck', ['npm', 'run', 'check:tests'], ROOT / 'web')
        run('Browser test/config typecheck', ['npm', 'run', 'check:browsers'], ROOT / 'web')
        run('Frontend correctness lint', ['npm', 'run', 'lint'], ROOT / 'web')
        run('Frontend format check', ['npm', 'run', 'format:check'], ROOT / 'web')
        run('Frontend component regression', ['npm', 'run', 'test:components'], ROOT / 'web')
        run('Frontend module boundaries', ['node', str(ROOT / 'scripts/frontend-boundaries-check.cjs')])
        run('Frontend gate negative controls', ['node', str(ROOT / 'scripts/frontend-tooling-check.cjs')])
        run('Verification gate unit', [sys.executable, '-m', 'unittest', 'discover',
                                      '-s', str(ROOT / 'scripts'), '-p', 'test_verify.py'])
        run('Immutable image gate unit', [sys.executable, '-m', 'unittest', 'test_image_gate'], ROOT / 'scripts')
        run('Artifact privacy gate unit', [sys.executable, '-m', 'unittest', 'test_artifact_privacy'], ROOT / 'scripts')
        run('Protected review admission unit', [sys.executable, '-m', 'unittest', 'test_image_admission'], ROOT / 'scripts')
        run('Backup snapshot unit', [sys.executable, '-m', 'unittest', 'test_backup_coherence'], ROOT / 'scripts')
        run('Backup manifest unit', [sys.executable, '-m', 'unittest', 'discover',
                                     '-s', str(ROOT / 'scripts'), '-p', 'test_backup_manifest.py'])
        run('Session identity unit', ['node', str(ROOT / 'scripts/security-identity-unit.cjs')])
        run('Installer unit', [sys.executable, '-m', 'unittest', 'discover', '-s', str(ROOT / 'deploy'), '-p', 'test_*.py'])
        run('Go unit (race)', [go, 'test', '-race', '-count=1', './...'])
        run('Localization and public issue unit', ['npm', 'run', 'test:i18n'], ROOT / 'web')
        run('Map layout unit', ['node', str(ROOT / 'scripts/map-layout-check.cjs')])
        run('Map links unit', ['node', str(ROOT / 'scripts/map-links-check.cjs')])
        run('Avatar initials unit', ['node', str(ROOT / 'scripts/initials-check.cjs')])
        run('Task workspace unit', ['node', str(ROOT / 'scripts/task-workspace-check.cjs')])
        run('History export and planning clock unit', ['node', str(ROOT / 'scripts/history-clock-check.cjs')])
        run('Bug Report metadata unit', ['node', str(ROOT / 'scripts/bug-report-check.cjs')])
        run('Bug Report encryption roundtrip', ['node', str(ROOT / 'scripts/report-crypto-check.cjs')], extra={'GMA_PYTHON_BIN': sys.executable})
    if args.suite in ('integration', 'all'):
        run('API integration (race)',
            [go, 'test', '-race', '-count=1', '-json', '-run', '^TestAPIIntegration$', './internal/planner'],
            extra={'GMA_TEST_DSN': dsn}, expected_test='TestAPIIntegration')
        run('Diagnostic API access integration (race)',
            [go, 'test', '-race', '-count=1', '-json', '-run', '^TestDiagnosticAPIIntegration$', './internal/planner'],
            extra={'GMA_TEST_DSN': dsn}, expected_test='TestDiagnosticAPIIntegration')
        for test in ('TestTrustedProxyIntegration', 'TestDatabaseRolesIntegration', 'TestDirectorySyncLifecycle',
                     'TestSecurityAuditAccess', 'TestSecurityAuditMatrix', 'TestSecurityAuditResilience',
                     'TestSecurityAuditRevocationSeatRace', 'TestPlaywrightMemberMilestoneRegression'):
            run(test + ' (race)',
                [go, 'test', '-race', '-count=1', '-json', '-run', '^' + test + '$', './internal/planner'],
                extra={'GMA_TEST_DSN': dsn}, expected_test=test)
        for test in ('TestReadPathRevisionFactsCoherence', 'TestReadPathSlowResponseReleasesCapacityLock', 'TestReadPathConcurrentCapacityInputs'):
            run(test + ' (race)',
                [go, 'test', '-race', '-count=1', '-json', '-run', '^' + test + '$', './internal/planner'],
                extra={'GMA_TEST_DSN': dsn}, expected_test=test)
        for test in FOCUSED_HISTORY_TESTS:
            run(test + ' (race)',
                [go, 'test', '-race', '-count=1', '-json', '-run', '^' + test + '$', './internal/planner'],
                extra={'GMA_TEST_DSN': dsn}, expected_test=test)
        run('Community backend build',
            [sys.executable, str(ROOT / 'scripts/build.py'), '--backend-only'])
        run('Signed release tamper regression', [sys.executable, str(ROOT / 'scripts/release-check.py')])
        run('Isolated database role upgrade', [sys.executable, str(ROOT / 'scripts/database-roles-check.py')])
        run('Online backup snapshot and failure cleanup',
            [sys.executable, str(ROOT / 'scripts/backup-coherence-integration-check.py')])
        recovery = [sys.executable, str(ROOT / 'scripts/recovery-integration-check.py')]
        if all(recovery_images.values()):
            for role, image in recovery_images.items():
                recovery.extend(['--' + role + '-image', image])
        run('Isolated backup recovery', recovery)
    if args.suite in ('e2e', 'all'):
        run('Frontend build for E2E', ['npm', 'run', 'build'], ROOT / 'web')
        run('Workspace refresh browser regression', ['node', str(ROOT / 'scripts/workspace-refresh-check.cjs')])
        run('History and planning timezone browser regression', ['node', str(ROOT / 'scripts/history-ui-check.cjs')])
        run('Multi-year browser regression', ['node', str(ROOT / 'scripts/multiyear-ui-check.cjs')])
        run('Updater browser regression', ['node', str(ROOT / 'scripts/updater-ui-check.cjs')])
        run('Session identity browser regression', ['node', str(ROOT / 'scripts/security-identity-regression.cjs')])
        run('Browser E2E (real API and PostgreSQL)',
            [go, 'test', '-race', '-count=1', '-json', '-timeout', '5m',
             '-run', '^TestBrowserE2E$', './internal/planner'],
            extra={'GMA_TEST_DSN': dsn, 'GMA_E2E': '1'}, expected_test='TestBrowserE2E')
        playwright()
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except subprocess.CalledProcessError as exc:
        print(f'FAIL: check exited with code {exc.returncode}', file=sys.stderr)
        sys.exit(1)
    except subprocess.TimeoutExpired:
        print('FAIL: check exceeded 600 seconds', file=sys.stderr)
        sys.exit(1)
    except OSError:
        print('FAIL: required executable or working directory unavailable', file=sys.stderr)
        sys.exit(1)
    except (RuntimeError, json.JSONDecodeError) as exc:
        print(f'FAIL: {exc}', file=sys.stderr)
        sys.exit(1)
