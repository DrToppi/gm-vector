# WP-04 verification record

Status: **IMPLEMENTATION STARTING — NO WP-04 IMPLEMENTATION ACCEPTANCE**.
Baseline: `d656d65`, containing independently accepted WP-02 implementation
`400ae188b6c059170dacfba7b3c8aec4a8af13af` and its documentation closure.
Task branch: `feature/history-and-timezones`.

The [bounded plan](wp-04-implementation.md), accepted
[D05 B/D06 A/D12 A](wp-04-decisions.md) and matrix 04-01–09 govern this package.
No new history, timezone or portable-import check is claimed as executed below.
Main and the running updater installation remain outside this work.

## Independent plan gate

A separate READ-ONLY reviewer checked accepted choices against current code and
found no architectural blocker. Before coding, the plan was refined to retain
NULL-employee facts, define raw distinct-task legacy sums, bound the export slot
through network-buffer lifetime, and specify zone-acknowledgement preview behaviour.
Browser output conversion and PDF DOM construction are bounded separately. Test
refinements include an internal DST transition with valid endpoints and unsupported
browser timezone data. This permits bounded implementation, not a PASS verdict.

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
