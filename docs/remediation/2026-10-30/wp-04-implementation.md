# WP-04 bounded implementation contract

Status: **BOUNDED PLAN FROZEN; IMPLEMENTATION AUTHORIZED**.
WP-02 is independently accepted at400ae188. A different READ-ONLY reviewer found
no remaining blocking plan contradiction after the documented refinements below.
This is a plan gate, not WP-04 implementation acceptance.
The user accepted D05 B, D06 A and D12 A in the
[decision packet](wp-04-decisions.md). No deployment or main merge is implied.

## Scope and invariants

Repair complete time-history access and define one installation planning timezone.
Keep scheduling/allocation, nominal minutes, actual/summary billing semantics,
TOTP/session/expiry durations, role/scope boundaries and the 5 MiB import limit.
Do not invent time facts or employee/date attribution for legacy minutes. Do not
reinterpret ISO date labels as instants, rewrite historical work dates or use local
midnight arithmetic that changes calendar-day lengths across DST.

Allowed modules: new history/cursor/export/timezone helpers; narrow API routes,
read authorization/configuration/startup/scheduler-clock/Graph-adapter changes;
TaskEffort/history/export/import/clock UI and seven catalogs; OpenAPI generator/
contract, installer/configuration examples and focused tests/runner wiring.
No new dependencies, state framework, retained snapshot/job system, intraday slot
allocation or live calendar enablement. A new additive index migration is permitted
only after representative query plans justify it; do not alter old checksums.

## Paged history API

Add `GET /projects/{id}/time-history`. Query: task_id, employee_id, date_from,
date_to, limit and cursor. Default limit 100, maximum 500. Task selection includes
the selected root and its current descendants; source task IDs/names remain visible.
An absent task filter means all project records. Filters apply before paging, and
must not require current assignment/participation or the current project date span.
Retained facts can concern removed assignees and historical dates outside a plan.

Freeze these shared DTOs (empty filter strings mean unbounded):

```ts
type HistoryFilters = {
  task_id: string; employee_id: string; date_from: string; date_to: string;
};
type HistoryItem = TimeEntry & {task_title: string; employee_name: string};
type HistoryCommon = {
  project_id: string; project_title: string; revision: number;
  planning_timezone: string; filters: HistoryFilters;
  order: 'work_date_desc_id_asc'; total_count: number; recorded_minutes: number;
  legacy_unattributed: {
    minutes: number; scope: 'project' | 'task_subtree';
    employee_date_attribution: 'unavailable';
  };
};
type HistoryPage = HistoryCommon & {
  limit: number; items: HistoryItem[]; next_cursor: string | null;
};
type HistoryExport = HistoryCommon & {
  format: 'gm-vector-time-history'; schema_version: 1;
  generated_at: string; items: HistoryItem[];
};
```

Source names are current labels, not historical-name snapshots. Legacy minutes
cover the selected root/subtree or project, independently of employee/date filters;
the UI must label that distinction rather than pretending those minutes match
the filters. Precisely: sum raw tasks.legacy_actual_minutes once per distinct task
in that selected scope, including root and descendants; never compute this as
summary Actual minus recorded minutes or roll up parent/child values a second time.
Keep the existing TimeEntry shape unchanged. Valid retained rows can have NULL
employee_id: preserve them with LEFT JOIN/COALESCE, empty employee_id/name fields
and a localized unknown-employee label, without inventing an identity.
Two empty employee IDs never imply ownership: nonmanager delete controls and
server checks require a nonempty linked employee matching the record. Managers
retain their existing ability to remove project records, including unattributed ones.

Return a documented envelope with items, next_cursor, current revision, exact
filtered total_count and recorded_minutes. Totals exclude the cursor position and
are independent of loaded pages. Legacy minutes remain separate and explicitly
unattributed; never label them as employee/date-filtered records or force recorded
totals to equal summary Actual. Preserve milestone's own old records plus children.

Use one short existing D11 shared-lock transaction for active-project existence,
current read permission, revision, subtree, filters, totals and rows. A write-only
lockedRole helper must not accidentally deny viewers; do not borrow another pool
connection for authorization from inside a held transaction. Materialize before
ending that transaction and release before output. Reauthenticate/recheck scopes
on every request; new endpoints use active-project404 semantics.

Preserve `work_date DESC, id ASC`. Keyset continuation is date<last_date OR
(date=last_date AND id>last_id). Read limit+1 to determine continuation. A cursor
is purpose-separated HMAC-SHA256 with constant-time verification, bounded encoded
size and canonical fields: version, project, principal/auth kind, normalized filters,
page size/order, project revision, last position and original fixed UTC expiry.
Expiry is 15 minutes, never sliding. No secrets/comments in the cursor or logs;
signing provides integrity, not confidentiality. Key rotation invalidates cursors.
Malformed/tampered/expired/foreign cursors fail explicitly; changed revision returns
409 history_changed. It is revision-guarded traversal, not a retained snapshot.
Use 422 invalid_cursor for malformed/tampered/foreign/context-mismatched cursors
and 422 cursor_expired for valid signed expired cursors. Authorization precedes
cursor/data details; revision invalidation precedes current subtree lookup.
Malformed filters/limits or reversed inclusive date ranges return 422 validation,
with no clamping. A fresh hidden/inactive project or unknown task root returns 404.
Repeat normalized filters and limit on continuation requests. An employee ID with
no retained matches produces an empty result without a global-directory lookup.

The legacy `GET /projects/{id}/time-entries` stays a bare array with its 1000-row
compatibility limit. Add explicit total/returned/truncated/revision/paging headers;
exactly 1000 does not mean truncated. Preserve documented legacy existence/archive
edge behaviour rather than silently changing it while introducing the new route.
Move current UI to the paged API. Preserve drafts and D03 request/session intent
guards, including late pages, fact refreshes, project switches and archived tasks.

## Complete separate history export

Add manager/export-scope `GET /projects/{id}/time-history/export`, returning a
separate versioned history JSON document with filters, planning_timezone, generated
UTC instant, exact recorded totals and all matching entries/source labels.
The UI derives standalone HTML, CSV and browser PDF using the established workflow.
It must escape text and protect spreadsheet formulas; never embed time facts in
ordinary Project JSON or create identities through import.
Browser conversion is bounded too: at most 64 MiB encoded HTML/CSV/JSON output,
with incremental byte accounting before concatenation; browser PDF additionally
limits print HTML to 8 MiB and 10000 rows before creating the print document.
Reject oversized conversion with filter guidance, never silently truncate or
render all history in the main page. Verify ordinary 10000-row short-comment PDF
and escaping expansion limits. Browser memory observations are diagnostic;
do not claim control over all browser/process allocations from a byte guard.

Read metadata and entries coherently in one protected operation, not by traversing
public cursors and pretending those requests are an atomic export. Materialize before
headers, release before download. Engineering guards to validate: one concurrent
export per API process, at most 100000 rows, at most 64 MiB encoded output and a
20-second materialization deadline. The existing server 30-second write deadline
remains. Keep the export slot until the encoded buffer's network-write/cancel
lifetime ends, although the database transaction/locks end before output. Do not
queue waiting exports: 429 history_export_busy. Resource exhaustion returns
422 history_export_limit; pre-output deadline exhaustion returns
504 history_export_timeout. Request cancellation does not produce a partial
successful document. These are engineering guards, not history retention limits.
Fail explicitly before output if exceeded, with filter/narrow-range guidance;
never truncate a file. Do not accumulate an unbounded row slice before checking
bytes or spill plaintext history into the 32 MiB container tmpfs. Verify ordinary
10000-row exports and worst legal individual comments/escaping, byte/row limits,
cancellation, concurrent changes and slow downloads; revise engineering guards
only with measured memory/correctness evidence. The API container limit is 768 MiB.
All history remains traversable through bounded pages beyond a single-export budget.

## Installation timezone

Add validated installation `planning_timezone` with explicit Europe/Moscow
compatibility default. Resolve IANA/tzdata at startup and fail visibly on invalid
values; reject host-dependent Local fallback. Bind one immutable resolved policy
to a request/calculation, including related projects. Do not mutate a shared Store
clock from unrelated App instances. Ensure packaged runtime timezone data works.

Publish top-level planning_timezone through authenticated `/auth/me` metadata;
it belongs to the installation, not User. Backend today, UI defaults/overdue,
reports and labelled timestamp presentation use that same zone. Preserve UTC
instants/security elapsed durations and ISO DATE strings/nominal minute arithmetic.
Guard timezone metadata against stale/session-crossing responses like other state.
Calculate today from the current instant when needed, rather than caching one day
indefinitely across midnight.
If browser Intl rejects a server-valid zone, surface the incompatibility explicitly;
do not silently fall back to the host zone or Moscow for planning decisions.
Configuration changes require explicit restart/reload instructions and describe
their today/future-calendar effect without rewriting history. Backups retain config.

Carry the configured zone in prepared busy blocks; validate provider identifiers
and detect nonexistent/ambiguous DST wall-clock windows before publication, warning
instead of silently choosing an instant. Keep live calendar/OAuth disabled; use fake
providers only. Do not blindly replace every Windows timezone with an arbitrary IANA
string or change allocation to elapsed DST minutes.
Check transitions inside a busy window as well as endpoint gaps/folds; valid
New_York01:00–04:00 endpoints can still cross a spring transition.

## D12 compatible project formats

Keep the existing project JSON export/default and strict legacy import structure.
Offer an explicit optional portable JSON format/resource with versioned metadata
containing planning_timezone and the same protected project payload. New import
supports both formats; an old importer is not expected to accept the optional new
envelope. Reuse existing identity/assignment/external-reference normalization.

Use `GET /projects/{id}/portable/export` for the optional document:

```ts
type PortableProject = {
  format: 'gm-vector-project'; schema_version: 1;
  metadata: {planning_timezone: string; generated_at: string};
  project: Project; // The same protected object as the legacy export.
};
type PortableImportRequest = {
  document: PortableProject; preview: boolean;
  timezone_acknowledgement?: {source: string; target: string};
};
```

New `POST /import/portable` strictly accepts this request with the same preview
option as legacy import; keep legacy `/import` decoding and default `/export` unchanged. UI format
detection chooses the correct endpoint. The new export route retains the existing
export-scope suffix guard. Request and OpenAPI schemas must describe actual objects.
Declared zone mismatch requires a server-validated explicit acknowledgement and
keeps date labels unchanged. Legacy files are labelled as lacking proven zone
metadata; do not fabricate provenance or silently convert them. No ZIP parser,
embedded time facts or weakened unknown-field/request-size checks.
Bind mismatch acknowledgement to both source and current target zone using
`timezone_acknowledgement: {source: string, target: string}`. Recheck it on import
so an earlier confirmation cannot silently survive a target-zone configuration
change. Do not require fictitious acknowledgement for a matching zone.
Preview has no persistence: with no acknowledgement it may return a mismatching
source zone and the fields source_planning_timezone, target_planning_timezone and
timezone_acknowledgement_required alongside the existing project/message fields.
Actual import requires the exact acknowledgement for a mismatch. An explicitly
supplied incorrect acknowledgement fails even on preview; it is never ignored.
The full nested request, including metadata, remains within the existing 5 MiB cap.

## Evidence and file ownership

Start from accepted WP-02 and record exact source. Reproduce truncation and planning-
zone failures before fixes; use matrix04-01 through04-09 with fixed clocks. Include
0/1/999/1000/1001/10000 rows, identical dates, root/subtree and removed assignees,
revision races, cursor tampering/expiry/access, exact totals, complete export values,
formula/XSS cases and all resource boundaries. Verify UTC/Moscow/New_York/Kathmandu,
midnight/leap/year/DST gaps/folds, different browser/server TZ and backup/import
without date or effort rewrites. Every supported locale must remain complete.

Backend owns Go/config/OpenAPI and backend fixtures; frontend owns web and relevant
browser/unit fixtures; root owns installer/backup Python, documentation, runner/CI
coordination, packaged-runtime verification and evidence.
Keep one writer per file. Independent read-only plan/code/exact-SHA review and final
CI are required; skipped tests or unrun provider connections are not passes.
