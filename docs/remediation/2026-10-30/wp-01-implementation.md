# WP-01 bounded implementation contract

Status: implementation authorized and started after updater deployment.
Accepted choices: D01 B (explicit annual versions plus provisional weekdays),
D02 B (calendar coverage plus bounded work). No change to effort semantics,
timezone policy, directory integrations or capacity-allocation policy.

## Data contract

Add optional Project.calendar_versions, a map from four-digit year to immutable
calendar ID. Retain calendar_id as the compatible anchor. Old records/payloads
without the map retain their existing anchor and any stored selections. Manager
or global administrator alone may change annual selections through preview/save
and optimistic revision checks. Importing a new calendar never silently attaches
it to a project. Existing annual calendars remain immutable.

A selected annual calendar must exist, match its map year, have all dates and a
valid checksum. A gap inside a verified year is an error, not provisional data.
An entirely unselected year uses Monday–Friday working days and emits a warning
for each actually consulted provisional year. Weekends, transferred working days,
shortened days and participation exceptions keep their existing semantics.

The supported date domain remains the calendar importer domain, years 2000–2200.
No fixed one-year/two-year task or project-span restriction remains. Leap dates,
working/calendar display modes, dependency shifts and both project/current-other
capacity calculations must share the same day lookup and provenance.

## Calculation guardrails

Engineering defaults to verify against fixtures and benchmarks: at most 2 million
calendar/assignment evaluations per coherent portfolio calculation, at most 100000
derived Load entries per project, and at most 16 MiB serialized derived ledger.
These are explicit safeguards under D02 B, not negotiated performance SLOs.
Keep the existing 5 MiB request limit and preserve previously accepted authoritative
metadata sizes; do not silently enlarge an API/security bound. Strip read-only
loads/warnings/provisional status from mutation payloads and importable JSON
exports, leaving the displayed project object intact. Do not store the large
derived ledger in new snapshots; retain authoritative fields and recompute it.
Before accepting values, run boundary and sparse-availability fixtures, record
time/memory and confirm representative 300-task multi-year plans remain useful.

Check request cancellation periodically. Exhaustion returns the explicit
calculation_budget_exceeded error; clear partial loads and calculated dates and
do not persist a partial plan. Include next-working/manual-duration/delay paths
and assignments in the work budget; do not replace 730 with an unbounded loop.

## Allowed changes

- New schema-008 migration for annual pins and the duration constraint; never
  modify old migration checksums. Update the embedded migration list.
- Go model/store/create/preview/save/import/export/snapshots, composite calendar
  loading, scheduler guards and relevant tests. Keep member permission boundaries.
- TypeScript Project types, project settings annual selectors, warnings and the
  task editor/Gantt clamp. Seven locale catalogs and OpenAPI.
- Only tests and necessary build/verification wiring beyond those components.

## Required evidence

Use the WP-01 matrix01-01 through01-12 in work-packages.md, frozen clock and
synthetic annual calendars. First reproduce the current annual coverage and731
day failures; then verify2025–2027, leap2028, customer/manual tasks, shared quotas,
missing/corrupt pinned years, provisional warnings, preview/cancel, role/revision
conflicts, old payloads and migration/backup/import/export compatibility. Test
100k Load/derived-result-size/work-budget boundaries with independent expected counts.

Record actual commands/results and limitations. Have a separate read-only reviewer
inspect the final implementation SHA. No WP-01 PASS or deployment is implied by
this plan. Keep the verified working updater installation intact during tests.


## Pre-implementation regression and plan review

Two baseline regressions were actually executed against the existing contracts:
three calendar days starting 2026-12-31 cannot reach 2027-01-04 with the single-year
calendar; a 731-day span starting 2026-01-01 is rejected by the fixed duration cap.
Both use existing baseline model types and a frozen 2025-01-01 clock, not a
compilation failure against new fields. Raw baseline logs remain private.

Independent plan review found an additional 740-column Gantt grid truncation;
bounded horizontal rendering must keep the full date range navigable. The initial
20k/4MiB-full-result guard proposal was revised before acceptance: it could reject
valid legacy 4.5 MiB metadata and dense multi-year projects. The bounded ledger
limits above must be tested with 300 tasks/approximately 30000 Load entries and a
legacy 4.5 MiB authoritative payload. These are engineering safeguards, not an
approved latency or throughput SLO.
