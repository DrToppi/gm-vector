# Baseline and findings

Baseline commit: `ab4abfee0bc6b8d16a6453fe3bae1958624af3d9`.
Public repository: https://github.com/DrToppi/gm-vector.
CI: [run 40959461361](https://github.com/DrToppi/gm-vector/actions/runs/40959461361),
completed successfully, checked through GitHub API on 2026-10-30.
This records existing evidence; no full suite was rerun for documentation edits.

The baseline is React/TypeScript + Go + PostgreSQL + nginx, Docker Compose,
separate runtime/migration/bootstrap DB roles, TOTP, local first administrator
setup without an installation key, SMTP and directory integrations.
See [architecture](../../architecture.md). Production/customer data is excluded
from all fixtures and public artifacts. This documentation changes no deployment.

## Evidence inventory

| Existing check | Location | Coverage / limitation |
| --- | --- | --- |
| Unified runner | production/scripts/verify.py | unit, integration, E2E; explicit isolated test database |
| Go units/race | production/internal/planner/*_test.go | scheduling, capacity, auth, role boundaries; not a full load benchmark |
| Browser E2E | production/scripts/e2e-check.cjs, production/internal/planner/e2e_test.go | real API/PostgreSQL with Chromium/CDP; does not demonstrate Firefox/WebKit support |
| Frontend build | production/web/package.json | TypeScript and Vite; no ESLint/Prettier/component test script in baseline |
| i18n checks | production/web/scripts/i18n-check.cjs | seven catalogs, parity/placeholders and selected behaviour |
| Image gate | .github/workflows/production.yml | Trivy HIGH/CRITICAL gate only on proxy; backend and PostgreSQL are built, not gated equivalently |
| Dependency checks | same workflow | npm audit and govulncheck; not a substitute for container or authorization checks |
| Installation | production/scripts/install-check.py | empty/repeated setup, HTTPS, TOTP, local recovery; not a historical-data migration matrix |
| Restore | production/scripts/recovery-integration-check.py | isolated backup/restore; extend with historical/volume/runtime fixtures |

## Findings: distinguish observed facts from risks

| ID | Observation and source | What is not yet proven |
| --- | --- | --- |
| F-01 | store.go/readCalendar loads one calendar ID; admin.go/validateCalendar requires exactly one year's dates; distribution has ru-2026.json only | Existing capacity tests include a synthetic horizon-exhaustion fixture, but no 2025–2027 acceptance fixture is established; importing another year alone does not attach it to a project |
| F-02 | schedule.go validates DurationDays<=730 and bounds next-working/capacity search at 730 days; schema-002.sql also has duration_days<=730, Editors.tsx max 730 and Chart.tsx clamps resize to 730 | These are distinct from annual-calendar coverage; raising a limit alone is not a fix |
| F-03 | main.tsx/open applies the awaited response directly; polling is every 5000 ms and guards current project ID but not monotonic revision | api.ts already has sessionEpoch/AbortController/session-scope guards and polling has cancellation; deterministic tests must prove remaining within-session/project ordering races |
| F-04 | store.go/prepare recalculates a project and other projects for capacity; projects read path must be profiled | No measured latency, query budget or bottleneck attribution is claimed |
| F-05 | time_entries.go returns ORDER BY work_date DESC,id LIMIT 1000 | Client/export completeness above1000 must be measured; not every view necessarily uses this endpoint |
| F-06 | store.go/prepare loads Europe/Moscow to derive now | Desired timezone meaning and all other temporal assumptions require inventory |
| F-07 | main.tsx, Editors.tsx, Panels.tsx and Chart.tsx mix substantial UI/orchestration logic; package.json lacks lint/format/component checks | Size alone does not justify rewriting; extract only approved boundaries |
| F-08 | CI uses Chromium; only proxy image gets required Trivy gate | No assurance for all-image vulnerabilities or other browser engines |

## Baseline capture required before each implementation WP

Record source SHA, clean-tree status, OS/CPU/RAM, Go/Node/Python/browser/PostgreSQL
versions, image digests, commands, fixture seeds, UTC start/end, exit codes and
skip counts. Capture database schema version and anonymized row counts from test
fixtures only. Record baseline timings/query counts before optimizing anything.
Keep raw private logs locally; publish only sanitized reproducible evidence.
