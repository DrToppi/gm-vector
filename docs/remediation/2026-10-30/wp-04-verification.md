# WP-04 verification record

Status: **IMPLEMENTATION STARTING — NO WP-04 IMPLEMENTATION ACCEPTANCE**.
Baseline: `d656d65`, containing independently accepted WP-02 implementation
`400ae188b6c059170dacfba7b3c8aec4a8af13af` and its documentation closure.
Task branch: `feature/history-and-timezones`.

The [bounded plan](wp-04-implementation.md), accepted
[D05 B/D06 A/D12 A](wp-04-decisions.md) and matrix 04-01–09 govern this package.
Executed checks are distinguished below from the remaining acceptance matrix.
Main and the running updater installation remain outside this work.

## Independent plan gate

A separate READ-ONLY reviewer checked accepted choices against current code and
found no architectural blocker. Before coding, the plan was refined to retain
NULL-employee facts, define raw distinct-task legacy sums, bound the export slot
through network-buffer lifetime, and specify zone-acknowledgement preview behaviour.
Browser output conversion and PDF DOM construction are bounded separately. Test
refinements include an internal DST transition with valid endpoints and unsupported
browser timezone data. This permits bounded implementation, not a PASS verdict.

## Demonstrated baseline failures

Before production changes at plan source efe1751, the writers actually executed:

| Regression | Observed baseline | Result |
| --- | --- | --- |
| TestTimeHistoryLegacyMetadataRegression | 3000 facts silently reduced to 1000, total/truncated headers absent | RED, assertion failure |
| TestPlanningTimezoneProviderRegression | A New_York busy block was published as Russian Standard Time | RED, fake transport assertion failure |
| Real React old_task browser case | Older task record beyond the project's first 1000 missing | RED |
| Real React milestone browser case | Descendant records excluded by exact-task filtering | RED |
| Real React planning_zone browser case | Fixed expected New_York 2026-10-29 produced Moscow 2026-10-30 | RED |
| Real React portable browser case | Metadata envelope went through strict legacy import route | RED |

These were actual assertions in the existing components/backend, not missing
symbols or compilation failures. No live Graph/directory/mail system was called.

## Installer and recovery fixture checkpoint

New-install configuration now explicitly records the compatible Europe/Moscow
default. The existing installer suite first failed at the missing field, then all
five tests passed after the narrow initializer change. This is installer component
evidence, not timezone/history acceptance. Python syntax compilation also passed.

The recovery fixture now prepares 1001 old leap-day facts, including a NULL employee
and a removed assignment, root/child raw legacy minutes, a private New_York config
and digest assertions. Its first execution used the previously built a2e07a binary:
restore reached the expected missing /auth/me timezone assertion. That old-binary
failure is not claimed as an exact-current-source regression. The final candidate
must still pass actual restored API/history/config checks and packaged-image tzdata
verification. The existing backup mechanism has not been rewritten.

## Required evidence before acceptance

- Reproduce project-wide 1000-row truncation hiding an older task and a mismatched
  planning day with fixed UTC instants before changing production code.
- Record exact fixtures, IDs/counts/digests, page traversal, independent expected
  totals, cursor/access failures and unchanged legacy array/JSON contracts.
- Prove revision invalidation and coherent exports using actual database barriers;
  include removal of assignees, milestone-own facts and retained old dates.
- Execute complete HTML/CSV/JSON/browser-PDF history paths, formula/XSS fixtures,
  ordinary 10000-row exports and actual row/byte/concurrency/cancellation guards.
- Record export memory observations and normal-read behaviour while exporting;
  do not confuse an engineering guard with a business/history retention limit.
- Verify the four-zone clock table, midnight/leap/year/DST cases, packaged tzdata,
  disabled/fake provider behaviour, portable mismatch acknowledgement and no
  historical date/minute or security-expiry rewrite.
- Run all seven locale checks, affected frontend browser regressions and the
  required prior API/security/recovery suites. Skips are not evidence of a pass.
- Obtain independent READ-ONLY review and successful final exact-SHA CI. Only then
  record `PASS — INDEPENDENTLY VERIFIED`, with limitations stated explicitly.
