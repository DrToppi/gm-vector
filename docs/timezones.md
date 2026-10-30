# Planning timezone and portable project files

Status: **WP-04 independently verified at e97971d**.
The accepted policy is one IANA planning zone for the installation, shared by all
projects and common employee daily ceilings. It does not introduce intraday
allocation or separate project/employee timezones.

## Configure an installation

Runtime config.json accepts:

```json
{"planning_timezone": "America/New_York"}
```

This is one field in the existing private configuration, not its replacement.
New installations explicitly initialize Europe/Moscow. Older files omitting the
setting use the same compatibility default. Invalid zones or host-dependent
Local are rejected; the application must not silently inherit the host timezone.
The packaged runtime must include timezone data.

A change requires an operator-controlled restart and browser reload. Back up
database and matching configuration first using the documented
[recovery procedure](recovery.md); preserve credentials, keys and file permissions.
Edit the existing private configuration, restage it and recreate API/worker using
the same approved package's installation procedure. Updating a mounted source
file alone is insufficient when init-secrets has already staged it into a volume.
Verify the authenticated `/api/v1/auth/me` top-level planning_timezone and reload
open clients. Do not switch image/version as an incidental part of a zone change.

The setting changes planning today, defaults, overdue decisions and labelled report/
timestamp presentation. It does not rewrite stored dates, task effort or UTC audit
instants. TOTP, session expiry and security elapsed durations retain their existing
instant-based semantics. Browser rejection of a server-valid zone must be visible,
rather than falling back to another planning clock.

## Dates and daylight saving time

An ISO work date such as 2024-02-29 is a civil label. Do not convert it from UTC
midnight to another zone. Calendar-day arithmetic and nominal activity minutes
remain unchanged across 23/25-hour DST days. Today is evaluated from the current
instant in the installation zone, including after midnight.

Calendar publication remains disabled. Prepared/fake-provider checks carry the
configured zone and reject ambiguous, nonexistent or transition-crossing busy
windows with warnings. Valid endpoints alone do not prove that a window avoids
a transition. Provider timezone identifiers require validation before any future
live integration; an IANA name is not an arbitrary Windows identifier replacement.

## Two project JSON choices

The default JSON download and legacy `/projects/{id}/export` API remain compatible.
Legacy files have no proven timezone metadata; the UI labels that uncertainty.
Do not infer a file's origin from the compatibility default.

Optional **JSON with metadata** uses
`GET /api/v1/projects/{id}/portable/export` with the same export permission:

```ts
type PortableProject = {
  format: 'gm-vector-project'; schema_version: 1;
  metadata: {planning_timezone: string; generated_at: string};
  project: Project;
};
```

The protected Project payload follows the same identity/assignment normalization
as ordinary export/import. It includes no time facts, credentials or access grants.
Older importers are not expected to understand this optional envelope; use default
JSON when exchanging with them.

The new UI detects the format and routes portable requests to
`POST /api/v1/import/portable`:

```ts
{
  document: PortableProject;
  preview: boolean;
  timezone_acknowledgement?: {source: string; target: string};
}
```

A preview may display a zone mismatch without persisting anything. Actual import
requires explicit acknowledgement of the exact source and current target zone.
An incorrect supplied acknowledgement fails; changing the target configuration
invalidates an earlier confirmation. Matching zones require none. Date labels
and recorded effort are never converted to disguise a mismatch.

Strict nested decoding, unknown-field rejection and the 5 MiB entire-request limit
remain. Legacy `/import` retains its original structure. Project import does not
restore complete time history; use matched database/configuration recovery for it.
