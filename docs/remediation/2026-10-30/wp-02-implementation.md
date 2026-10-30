# WP-02 bounded implementation contract

Status: **IMPLEMENTATION STARTING AFTER WP-01 ACCEPTANCE**.
The user explicitly selected [D03 A/D04 A](decisions.md). This document bounds
this package. WP-01 was independently accepted at 5812b2c; numeric performance
SLOs still require explicit agreement after baseline measurements.

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

The accepted product envelope is up to 10 simultaneous users and 300 tasks per
project. The proposed five projects/30 employees, p95 limits and long soak duration
are not approved gates. Measure a clearly labelled baseline first, report actual
sample counts/errors/distributions, then obtain explicit agreement on numeric
performance acceptance before claiming WP-02 PASS. No implementer may turn
engineering observations into a negotiated SLO.

## Primary-source check

Checked on 2026-10-30: [React effect setup/cleanup](https://react.dev/reference/react/useEffect),
[document visibility](https://developer.mozilla.org/en-US/docs/Web/API/Document/visibilityState),
and [PostgreSQL isolation](https://www.postgresql.org/docs/current/transaction-iso.html).
Effects require symmetric cleanup; visibility changes can pause refreshes; default
database isolation alone does not establish a coherent multi-statement input view.
These sources inform implementation review, not a new architecture selection.
