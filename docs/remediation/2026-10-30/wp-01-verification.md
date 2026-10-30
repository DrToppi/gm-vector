# WP-01 verification record

Status: **IMPLEMENTED / INDEPENDENT ACCEPTANCE PENDING**. All fixtures below use
synthetic projects and identities. No working installation or customer data is
used by these checks. Main is unchanged; implementation is on
`feature/multiyear-planning`.

## Contract and changes

[D01 B/D02 B](decisions.md) and the [bounded contract](wp-01-implementation.md)
govern this package. Schema 008 adds annual calendar pins and removes the 730-day
database constraint without changing historical migration checksums. The existing
anchor remains compatible; omitted annual maps inherit prior selections, including
the former implicit anchor when a legacy client changes its year. Explicit empty
maps retain their clearing semantics. No effort, timezone or allocation policy is
changed.

The composite calendar distinguishes verified versions, selected-but-invalid
versions and unselected provisional weekdays. Calendar loading, graph expansion,
day and assignment work share one cancellable portfolio budget. Failure withholds
derived schedules, preserves authoritative input and never saves partial results.
Large derived ledgers are excluded from new snapshots, writes and importable JSON;
the existing 5 MiB authoritative request boundary remains in place.

The frontend supports long task durations and navigates the full multi-year Gantt
range through bounded horizontal rendering. Annual version changes use preview;
importing a calendar does not attach it automatically. Until selected calendar
data arrives, the chart shows unknown days and blocks date gestures. Other task
fields remain editable. All seven locale catalogs cover the new product text.

## Reproduction and review findings

The original single-year/730-day baseline was actually tested: a task crossing
2026-12-31 failed on missing 2027 data, and a 731-day duration was rejected. The
browser fixture also failed against the pinned updater baseline at the 731-day
editor assertion. These were executed failures, not conclusions from inspection.

Independent review then identified three defects, each corrected with regressions:

- Calendar validation/merging initially escaped the shared work budget. A cancelled
  loader regression demonstrated an unwanted SQL read; cancellation now precedes
  SQL, and annual validation/merge work is reserved before its loops.
- An unloaded selected anchor was initially treated as provisional weekdays in
  the chart. A held calendar-response fixture now checks neutral holiday display,
  blocked gestures, continued title/hour editing and restored verified dates.
- A legacy anchor-year change initially dropped the formerly implicit verified
  year. API regression first observed April 1 instead of the verified April 2;
  the fix retains that year only for an omitted map and changed anchor. Same-year
  replacement, unchanged legacy member saves and explicit clearing are checked.

The updater negative-test client also needed to tolerate an early BrokenPipe
during body transmission. The test still reads and asserts the actual HTTP
rejection and verifies that no engine operation was submitted.

## Evidence so far

| Check | Actual result | Boundary |
| --- | --- | --- |
| CI on `6fc9b1c953cf1fa3c7a86a514efaf6289bcd5f9a` | [SUCCESS](https://github.com/DrToppi/gm-vector/actions/runs/41016195913) | Superseded by the later compatibility and coverage commits |
| Full local `verify.py all` with isolated PostgreSQL on `1ab865c` | PASS | Unit/race, API, roles/security, recovery, builds, calendar/updater/identity browser fixtures and real browser E2E; later test additions need their own execution |
| Distinct-version Git/Docker upgrade, schema 7 to 8, on `6fc9b1c` | PASS | Actual old image replacement, 731-day save, failed migration, encrypted snapshot restore, TOTP/key retention and retry |
| Legacy anchor compatibility | RED then GREEN | Real API/race, including cross-year preview/save, same-year replacement, explicit clearing and member compatibility |
| Calendar loading and shared work budget | RED then GREEN | Cancel before SQL, 201 pins, shared budget and cancellation between years |
| Dense native scheduler benchmark | Measured, not an SLO | 300 tasks, 30 employees, 30000 loads: three iterations, about 94 ms/op and 29 MB allocated/op; excludes HTTP/SQL/concurrent load |

The full local browser E2E emitted an existing generic `operation_failed` server
event while its assertions passed. Its cause is not established by this package;
do not relabel that diagnostic as an expected success or suppress it.

Raw logs remain private. Final acceptance must identify the exact reviewed SHA,
executed commands and sanitized artifact hashes. An earlier CI success alone does
not accept later changes.

## Remaining acceptance work

The matrix gaps identified in independent review now have executed evidence:

| IDs | Executed regression and assertions | Result |
| --- | --- | --- |
| 01-01, 01-02, 01-05 | Verified 2025–2027 chain and leap date; six cross-year together/after/delay cases in both modes with literal expected dates and complete ledgers | PASS, Go race; the new ten-case subset was also run independently |
| 01-03 | Four individual-availability cases: shortened December 31, date overrides, transferred Saturday, holiday Monday, full-day and one-minute remainder | PASS, Go race |
| 01-04 | Actual API/DB portfolio with different annual anchors/pins; exact 480-minute global boundary, January 4 conflict at 600 minutes, recovery without quota changes, GET leaves persisted data unchanged | PASS, API race; independently rerun |
| 01-06, 01-09 | Missing/gapped/corrupt/version-conflicting years, provisional replacement, preview/cancel/save, undo of empty/pinned maps, stale revision, manager/member and legacy anchor behaviour | PASS, scheduler/API/browser |
| 01-07, 01-11 | Actual legacy 730-day migration and 731-day save, editor and resize; third-year chart navigation with bounded columns; unknown selected-calendar readiness | PASS, isolated Docker/API/browser |
| 01-08, 01-10 | Old schema 7 upgraded to 8; import and persisted re-export compare pins, tasks, dependency and exact dates; rich encrypted recovery compares calendar IDs/pins, revision, task facts, assignments, participants, dependency, loads and time-entry history | PASS; final CI remains pending |
| 01-12 | Dense 300-task/30000-load calculation, large legacy metadata, shared calendar/graph/allocation budgets, cancellation, derived count/byte bounds and nonpublication of partial results | PASS, focused and full local suite; not a load/soak SLO |

Full local `verify.py all` also passed on
`a2e07ade1506d5bdda82a821aafd9aa8a9bb51c1`, including the new matrix cases. Local
runtime: Linux amd64, Go 1.27.3, Node 24.21.0, Python 3.14.4 and Chrome
156.0.8078.73. Integration tests create unique schemas in the explicit disposable
test database. They do not use the working instance. Reproduce with:

```sh
python3 production/scripts/verify.py all --dsn-file "$TEST_DSN_FILE"
GM_VECTOR_APP_IMAGE="$BASELINE_IMAGE" python3 production/scripts/updater-integration-check.py \
  --baseline-source "$PINNED_UPDATER_SOURCE" --fetch-ref feature/multiyear-planning --multiyear
```

CI on that SHA passed browser checks but exposed a test-fixture port race in the
HTTPS Docker regression. Earlier two runs stopped at the new browser harness's
Chrome startup assertion. Acceptance remains pending until fixture diagnostics and
port allocation are corrected, affected checks execute, and final CI/review pass.

Ordinary import intentionally rejects employee participation and clears assignments;
this pre-existing security/identity contract remains unchanged. Its calendar/task
roundtrip is tested separately from full backup/restore, which preserves identities,
participation, assignments and time history. Do not infer full identity portability
from an HTTP 201 import response.

Whole-application operational acceptance remains open until the later WPs and
combined security, browser, load and recovery gates are complete. WP-01 is not yet
deployed to the working installation.
