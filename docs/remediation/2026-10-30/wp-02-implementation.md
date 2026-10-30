# WP-02 bounded implementation contract

Status: **IMPLEMENTATION STARTING AFTER WP-01 ACCEPTANCE**.
The user explicitly selected [D03 A/D04 A](decisions.md). This document bounds
this package. WP-01 was independently accepted at 5812b2c. Following the first
baseline, the user approved the numeric criteria recorded below.

## Refresh contract

Preserve the existing API session generation, abort, CSRF, scope and role checks.
Within a workspace, track selection intent separately from project ID: A→B→A is
a new selection generation. A superseded response or error cannot publish project,
role, employees, calendars, lists, preview or operation state into a newer intent.
Lower project revisions cannot replace higher revisions. Equal revisions remain
eligible only for the current request ticket: employee limits, other-project
capacity and roles can change without increasing this project's revision.
Project revision/ETag must not be used as a full derived-state cache validator.

At most one ordinary background refresh batch runs per workspace. Replace async
setInterval with a completion-scheduled five-second timer; pause while hidden,
offline, editing, previewing or writing. Coalesce visibility/reconnect catch-up.
Superseded selection reads are aborted and their publication invalidated; an
old transport ignoring abort must not prevent opening another project forever.
This exception is not permission for overlapping background polling.

Starting a write invalidates older reads. Mutations are never automatically
retried. Their success/error/finally callbacks must respect session and selection
intent. A successful write in an old selection may have persisted on the server;
its callback must not replace the currently selected project. Preserve visible
optimistic conflicts and unsaved task title/hour edits when refreshing time facts.
The project/role/associated employee result must share one acceptance guard.
List refresh has its own ordering guard because project revisions do not version
the employee or calendar catalogs. Resources shared by list and project refreshes
also need ordering across those request families: an older project poll must not
overwrite employees accepted by a newer list refresh (or vice versa). Separate
per-family tickets alone do not establish that guarantee. Cover this interleaving
with a held-response regression.

## Read-path contract

Keep on-demand calculation and existing shared/exclusive capacity locks. Profile
query count, calculation time, allocations and full HTTP reads before optimizing.
Use synthetic reproducible fixtures and compare the same machine/data/build.
Do not replace this with global caches or persisted derived schedules. A request-
scoped reuse of immutable calendar data or batched reads may be considered only
after profiling identifies repeated work; it must preserve validation, cancellation,
budget bounds, input coherence and independent composite day maps.

GET must not modify persisted project state. Existing shared quotas, holidays,
individual schedules, provisional-year provenance and conflict behaviour remain
unchanged. Do not add a schema/index unless actual query plans justify it. Do not
filter relevant projects using stale calculated end dates or trust user-supplied
derived ledgers. Any newly discovered consistency defect must be reproduced and
reviewed against existing transaction/lock contracts before changing them.

## Scope and verification

Allowed: a small frontend refresh coordinator/hook, Workspace integration, focused
read-path/store/calendar changes supported by measurements, synthetic query/load
harnesses, existing runner/CI wiring and relevant documentation. No mass component
rewrite, dependencies, new event transport, API cache contract, timezone or
allocation-policy change. Assign non-overlapping files to implementers; independent
review remains read-only.

Execute [matrix 02-01 through 02-12](work-packages.md). First establish deterministic
red regressions for stale selection/equal-revision derived responses, then test
poll-after-save, drafts, hidden/offline/slow reads, stale errors and same-account
relogin. Use held response barriers rather than arbitrary sleeps; send no-store
headers before withholding JSON bodies. Keep existing identity and capacity E2E
coverage. Backend evidence includes actual query counters/plans, persisted-state
digests, cold/repeated reads, changed calendars/quotas and concurrent saves.

The accepted WP-02 reference workload is five projects × 300 tasks, 30 shared
employees and ten distinct concurrent users. The user approved read p95 ≤500 ms,
preview/save p95 ≤2 seconds and zero unexpected errors, with ten minutes of mixed
load plus fifteen minutes of soak. Keep the sixty-minute soak in final WP-05.
Freeze the remaining reproducibility controls before executing the long run;
report actual sample counts/errors/distributions. No implementer may silently
weaken thresholds or turn a different experiment into an accepted SLO.

## Baseline experiments (not acceptance targets)

Use one and five ready projects, each with 300 tasks and 30 shared employees.
Each employee has a ten-task chain (270 links/project), 192 planned minutes per
task, a 96-minute task/day limit and a 96-minute project/day quota. Five projects
sum to the 480-minute global ceiling without deliberately triggering an early
capacity-conflict exit. Start across the 2026–2027 boundary with two independently
constructed verified annual versions and explicit holiday/availability controls.
Compare empty and 3000-row time-fact datasets. Use 1, 5 and 10 distinct authenticated
users; ten calls bypassing auth or sharing one session heartbeat row are not an
equivalent experiment.

Count actual normalized SQL statements without arguments using test-only pgx
tracing. Report full HTTP distributions and separate read/calendar/calculate/JSON
work, allocations, pool waits and lock lifetime. Obtain representative EXPLAIN
ANALYZE/BUFFERS only in a disposable schema. Record project-state digests before
and after reads, excluding expected authentication-session heartbeat updates.
Cold means a newly started test process, not a claim to have flushed database or
OS caches. Race-enabled correctness results are separate from latency profiles.

Reproduce two read-path hypotheses with controlled barriers before classifying
them as defects: time-entry commit between project revision and aggregate reads;
and a slow response writer holding the capacity transaction after materialization.
Do not select a new isolation or locking architecture solely from these hypotheses.
The old opt-in 20-project/100-employee test with a hardcoded 500 ms assertion is a
historical stress harness; it is not an approved WP-02 acceptance profile.

## Primary-source check

Checked on 2026-10-30: [React effect setup/cleanup](https://react.dev/reference/react/useEffect),
[document visibility](https://developer.mozilla.org/en-US/docs/Web/API/Document/visibilityState),
and [PostgreSQL isolation](https://www.postgresql.org/docs/current/transaction-iso.html).
Effects require symmetric cleanup; visibility changes can pause refreshes; default
database isolation alone does not establish a coherent multi-statement input view.
These sources inform implementation review, not a new architecture selection.
