# Work packages and verification matrix

Tests below are acceptance requirements. They are **planned, not executed** unless
the corresponding WP verification record explicitly reports actual evidence.
See [WP-01 verification](wp-01-verification.md). Expected results must
be encoded as assertions with independent expected values, not copied from the
algorithm under test. Use a frozen clock and explicit timezone in date-sensitive
fixtures; historical 2025–2027 tests must not depend on the real execution year.
Shared architectural decisions are in decisions.md.
WP-03 and WP-05 matrices are in [wp-03-05.md](wp-03-05.md).

## WP-01 — multi-year planning (P1)

Allowed: calendar selection/version contracts, schedule/calendar loading,
bounded horizon handling, necessary API/UI/migration changes and regression tests.
Excluded: changing effort semantics, timezone policy, official holiday invention,
unrelated chart redesign, capacity-allocation policy changes.
Decisions: D01 calendar coverage; D02 horizon/resource guard.

Invariants: holidays/weekends and individual activity windows remain effective;
working-day and calendar-day durations retain their distinct meanings; no silent
partial schedule; no mixing contradictory versions for the same date; calendar
versions/checksums remain reproducible; global employee maximum still holds across
projects. A missing future calendar is handled according to an approved policy,
never silently presented as an official calendar.

| Test ID | Fixture / action | Required assertions |
| --- | --- | --- |
| 01-01 | Synthetic complete annual calendars 2025,2026,2027; chain crosses both year boundaries | Exact expected starts/ends and daily minutes; all dependencies satisfied; no missing-date failure |
| 01-02 | Finish-start, together and delayed links over Dec 31/Jan 1 in both display modes | Mode-specific expected dates, no off-by-one shift; holidays honoured |
| 01-03 | Individual weekday windows, date exception, transferred working Saturday and holiday Monday | For allocatable internal tasks, allocation only on jointly available dates and totals equal planned minutes; preserve manual/customer task semantics |
| 01-04 | Two projects sharing employee capacity across year boundary | Per-project quota and global daily ceiling; deterministic accepted conflict behaviour |
| 01-05 | Leap day 2028 and non-leap 2027, month ends, timezone-independent date arithmetic | No nonexistent dates; exact day counts; no timestamp-derived drift |
| 01-06 | Missing year, gap inside year, overlapping/duplicate versions, checksum mismatch | Explicit approved error/provisional state; no partial published dates or silent version selection |
| 01-07 | Task duration729/730/731 days and approved horizon boundary±1; long no-capacity span | Exercise SQL constraint migration, task editor input and Gantt resize too; valid long tasks work; explicit bounded failure, no truncated effort |
| 01-08 | Existing single-calendar project, completed tasks, manual/customer dates, fixed deadline | Migration preserves IDs/revisions/history/hours/statuses; only approved scheduling changes occur |
| 01-09 | Calendar replacement/recalculation preview, accept/cancel, stale revision conflict | Cancel leaves persisted plan untouched; accept atomic; competing edit not overwritten |
| 01-10 | Export/import, encrypted backup/restore of multi-year project | Same versions, dates, dependencies, participants and hours after roundtrip |
| 01-11 | Gantt, map, task editor and reports spanning2025–2027 | Full date range visible/navigable; resizing/links and labels work; no UI range truncation |
| 01-12 | Sparse availability and large approved horizon with maximum agreed task count | Measure CPU/memory/runtime and API body/import/export/load-array sizes within D02 budget; cancellation and failure remain atomic |

Acceptance: synthetic 2025–2027 project and migration fixtures pass API, scheduler
and browser tests; reviewer checks calendar provenance separately from fixture
logic. Actual production calendars must be imported from verified sources under
approved policy; synthetic calendars must never ship as official holiday data.
Evidence: before/after schema/data manifest, reference date table, per-day load
ledger, export/restore comparison, UI artifacts, bounded-runtime measurements.

## WP-02 — state consistency and performance (P1)

Allowed: request sequencing/cancellation, revision checks, refresh coordination,
read-path computation/query optimization, justified indexes/cache invalidation,
concurrency and load harnesses. Excluded: changing authorization or scheduling
results, replacing the database/framework, hiding load by disabling correctness checks.
Decisions: D03 refresh strategy; D04 measured workload/SLO and cache boundaries.

Invariants: old responses never replace a newer revision; a response for project A
cannot populate project B; account/logout boundaries invalidate pending requests;
unsaved edits and preview are not overwritten; optimistic-write conflicts remain
visible; derived capacity is based on a coherent version of relevant inputs;
GET must not silently mutate persisted project state. Cancellation alone does not
establish ordering correctness; both revision and identity boundaries need tests.

| Test ID | Fixture / action | Required assertions |
| --- | --- | --- |
| 02-01 | Defer request A, complete newer request B, then release A | Final project/employee state remains B; old role/error cannot overwrite new state |
| 02-02 | Switch A→B, A→B→A during slow reads; return out of order | Only current selection/generation updates UI, including equal-looking revisions |
| 02-03 | Poll starts; user saves successfully; older poll returns | Saved revision and task values retained, no visual revert |
| 02-04 | Overlapping polls, hidden tab, offline/reconnect, slow server | At most approved in-flight work; cleanup/backoff; correct catch-up without request storm |
| 02-05 | Open editor/preview, edit unsaved fields, receive refresh, cancel/confirm | Draft not replaced; conflict shown; server truth applied only through approved path |
| 02-06 | Logout/login as another user while requests resolve or fail | No previous-user data, role, error, cache or toast crosses session |
| 02-07 | Two clients save same revision; calendar/quota changes race with preview/save | One accepted state or explicit conflict; no lost updates; authoritative capacity checked |
| 02-08 | Raw project read with query/Calculate counters | Measured costs and persisted-state digest; no read-triggered hidden mutation |
| 02-09 | Cold/warm load, repeated GET, changed task/calendar/quota/global maximum | Any cache invalidates for every dependency; no stale cross-project capacity |
| 02-10 | Agreed10 concurrent users,300 tasks/project; multiple projects, shared employees | Latency/error/throughput/query/CPU/RAM distributions reported under approved D04 |
| 02-11 | Burst and sustained soak; one slow/dead client; timeout/cancel | Bounded goroutines/connections/memory, recovery to baseline; no race detector failures |
| 02-12 | EXPLAIN(ANALYZE,BUFFERS) on representative synthetic dataset before/after | Index benefit demonstrated; no unbounded N+1 or regression in writes/migrations |

The user accepted the WP-02 reference profile after baseline measurements:
10 distinct concurrent users, 5 projects×300 tasks, 30 shared employees; ten minutes
of mixed load and fifteen minutes of soak. Read p95≤500 ms, preview/save p95≤2 s,
zero unexpected errors and no correctness failure. The sixty-minute soak remains
a final WP-05 requirement. Record bounded resource trends rather than mistaking
intentional persisted writes or client/server instrumentation for an application
leak. Ten projects remain an optional stress experiment, not product capacity.
The exact CPU/RAM, 90/5/5 mix, pacing and loopback-TLS latency definition are frozen
in [the bounded contract](wp-02-implementation.md). Report sample counts and
p50/p95/p99, not just averages; compare the same machine/data/harness/build settings.

Acceptance: deterministic race fixtures pass repeatedly without arbitrary sleeps;
the approved load targets met; results match baseline scheduling logic.
Evidence: replay seed, request trace with sanitized IDs, raw timing histogram,
query counts/plans, CPU/RAM/goroutine series and regression comparison.

## WP-04 — history and time semantics (P2, before WP-03)

Allowed: time-entry pagination contract/UI/export, stable sorting/filtering,
explicit timezone/date semantics and compatible migrations. Excluded: changing
billing, rounding, working-hours or employee-allocation policies without approval.
Decisions: accepted D05 B pagination/revision consistency, D06 A installation
timezone and D12 A optional metadata JSON. See the [bounded plan](wp-04-implementation.md).

Invariants: no silent1000-row truncation; deterministic total order with unique
tie-breaker; totals do not depend on loaded page count; access checked on every
page/cursor; date-only work dates are not silently converted between zones; UTC
instants and local calendar dates are distinct types/semantics in API contracts.

| Test ID | Fixture / action | Required assertions |
| --- | --- | --- |
| 04-01 | 0,1,999,1000,1001,10000 entries; many same-date rows | All accessible rows traversable exactly once while the revision is unchanged; an intervening revision requires explicit refresh |
| 04-02 | First/last/empty page, invalid/tampered/foreign cursor and page-size bounds | Defined error, bounded resource use, no cross-project leak |
| 04-03 | Insert/update/delete while paging and exporting | Revision-guarded pages fail explicitly on change; one coherent full export, no unexplained duplicates/gaps or mixed export |
| 04-04 | Filter/sort/page navigation then reload | Stable ordering, correct filtered totals and cursor reset; last-page behaviour defined |
| 04-05 | HTML/CSV/JSON/PDF exports with >1000 entries | Complete agreed dataset, totals reconcile; escaping/formula injection controls retained |
| 04-06 | UTC vs Europe/Moscow vs America/New_York vs Asia/Kathmandu | Approved timezone decides today/overdue/day boundary; date-only values preserved |
| 04-07 | DST gap/fold, leap day, year boundary, midnight, different browser/server TZ | Deterministic intended dates; timestamps roundtrip; no skipped/doubled effort |
| 04-08 | Existing projects with no timezone metadata; migration twice | Explicit compatible default; no rewrite of historical work_date or UTC audit timestamps |
| 04-09 | Backup/import/export between hosts with different timezone settings | Backup preserves history/config; legacy project JSON remains compatible without facts; portable JSON carries zone and requires source/target mismatch acknowledgement without rewriting dates |

Acceptance: history completeness, exact aggregate reconciliation and timezone
reference table pass API/browser/export/restore checks. Evidence includes expected
IDs/counts/digests and input→expected date tables for all temporal boundaries.
