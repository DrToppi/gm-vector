# Complete time history

Status: **WP-04 candidate contract; independent implementation acceptance pending**.
See the [bounded plan](remediation/2026-10-30/wp-04-implementation.md) and
[verification record](remediation/2026-10-30/wp-04-verification.md).

## Records and totals

The task card shows server-filtered history. Selecting a milestone includes its
own retained records and every current descendant. Source task and employee
labels identify each record; labels are current names, not historical snapshots.
An unknown employee remains visible rather than causing the record to disappear.
Removing an assignment does not remove its historical records.

Recorded totals cover all matching records, independently of the loaded page.
Legacy minutes appear separately: these are raw legacy values summed once per
task in the selected project or subtree. They have no reliable employee/date
attribution and do not become filtered facts. Summary Actual still follows the
existing child rollup, so it need not equal history records plus root legacy.

## Integration resource

Use authenticated `GET /api/v1/projects/{id}/time-history` with project read
permission. Scoped API tokens require `project:read` and access to this project.
Query parameters:

| Parameter | Meaning |
| --- | --- |
| task_id | Optional root-inclusive current task subtree; omitted means project |
| employee_id | Optional retained employee ID; no current assignment required |
| date_from / date_to | Inclusive ISO date labels, independent of project span |
| limit | Default 100, maximum 500; invalid values are rejected |
| cursor | Opaque continuation context from the preceding page |

The response includes items, next_cursor, revision, filters, limit, project context,
planning_timezone, total_count, recorded_minutes and legacy_unattributed. Order is
`work_date DESC, id ASC`. An empty next_cursor ends traversal. Retain normalized
filters and limit for every continuation request.
The UI renders one page and retains at most 100 backward cursor positions. A notice
identifies discarded navigation positions; First can restart browsing earlier
records. Forward access and history retention are not limited by this guard.

A cursor expires 15 minutes after the first page and never extends its own life.
It is bound to the authenticated principal/kind, project, revision, filters and
ordering. Every page rechecks current authorization. Signing protects integrity,
not confidentiality: do not use cursor contents as a secret store or log them.

| Error | Client response |
| --- | --- |
| 409 history_changed | Project revision changed; explicitly refresh from the beginning |
| 422 cursor_expired | Context expired; start a new traversal |
| 422 invalid_cursor | Malformed, altered or mismatched context; discard it |
| 422 validation | Correct filters, limit or reversed date range |
| 401 /403 /404 | Reauthenticate or obtain access; never reuse another project's cursor |

This is revision-guarded traversal, not a retained snapshot. Even an unrelated
project edit can require refresh. A deleted/moved task during traversal triggers
revision invalidation before the current subtree is evaluated.

The legacy `/time-entries` resource remains a bare array capped at 1000 records.
X-History-Total-Count, X-History-Returned-Count, X-History-Truncated,
X-History-Revision and Link make that compatibility limit explicit. Exactly 1000
records is not truncation. New clients should use `/time-history`.

## Complete history downloads

The history export is separate from project export. Manager/export-scope
`GET /api/v1/projects/{id}/time-history/export` accepts the same task/employee/date
filters and returns versioned `gm-vector-time-history` JSON with all matching
records, exact totals and metadata. It is one coherent read, not a series of page
requests labelled as an atomic export. The task-card export workflow also offers
HTML, spreadsheet-safe CSV and browser PDF. Loaded pages do not limit a download.

Engineering limits prevent unbounded resource consumption:

- Per API process: one concurrent export, 100000 rows, 64 MiB encoded document,
 20 seconds to materialize. Database locks end before network download; the export
 slot remains held until writing/cancellation releases its buffer.
- Browser conversion: 64 MiB encoded output; PDF additionally allows 10000 rows
 and 8 MiB print HTML before creating the print document.

Busy exports return 429 history_export_busy. A server limit returns 422
history_export_limit; materialization timeout returns 504 history_export_timeout.
Narrow the filter/range when a server or browser guard is reached. A file is never
silently truncated. All records beyond a single-export guard remain available
through pages and narrower exports. These guards do not delete or retain history.

HTML escapes user text; CSV protects spreadsheet formulas. Review a download before
sharing it: history intentionally contains authorized task names, employee labels,
comments and dates. It is not a sanitized Bug Report.

Ordinary and metadata project JSON contain no time-entry history. Importing them
does not recreate facts or employees; see [planning timezones](timezones.md).
