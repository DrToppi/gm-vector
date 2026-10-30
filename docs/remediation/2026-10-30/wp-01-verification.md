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

Independent matrix review requires explicit cross-year dependency-mode and
individual-availability tests, a real two-project capacity fixture, export/import
value comparison, and backup restoration of explicit annual pins with assignments,
participants and time facts. These checks are being added; they are not PASS from
the table above.

Ordinary import intentionally rejects employee participation and clears assignments;
this pre-existing security/identity contract remains unchanged. Its calendar/task
roundtrip is tested separately from full backup/restore, which preserves identities,
participation, assignments and time history. Do not infer full identity portability
from an HTTP 201 import response.

Whole-application operational acceptance remains open until the later WPs and
combined security, browser, load and recovery gates are complete. WP-01 is not yet
deployed to the working installation.
