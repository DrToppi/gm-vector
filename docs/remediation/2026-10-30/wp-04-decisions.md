# WP-04 history and timezone decisions

Status: **D05 B / D06 A ACCEPTED BY THE USER ON 2026-10-30**.
Prepared during the quiet WP-02 reference run. Implementation begins only after
WP-02 acceptance and a bounded WP-04 plan. The general programme is authorized;
accepting these choices does not waive verification or authorize deployment.

## Observed contracts

The history endpoint returns a bare array ordered by work_date descending and ID
ascending, silently capped at 1000 project-wide rows. TaskEffort filters that array
by task afterwards, so an older task can appear to have no history. Actual totals
already aggregate every row, independently of that limit. Actual includes legacy
minutes; summary tasks roll up their children. Preserve that calculation policy.

HTML/CSV/PDF/project JSON currently contain task totals, not individual time facts.
Ordinary project import creates no time facts and has employee/identity restrictions.
Do not add history to importable Project JSON or remove its 5 MiB protection.
Time-entry IDs are random, with no creation sequence: a date/ID watermark alone
cannot exclude new backdated rows or preserve deleted records.

Dates without times are PostgreSQL DATE / ISO labels; UTC-midnight values in date
arithmetic are surrogates, not event instants to convert into a viewer timezone.
The current planning clock is hardcoded to Moscow, while timestamp display often
uses browser local time. Capacity aggregates employee plus date, without intraday
slot allocation. Participation windows count nominal HH:mm minutes. A different
project zone cannot simply be added without defining shared-employee day semantics.

## D05 — exactly three history strategies

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — offset pages with revision guard | Simple numbered navigation; no retained snapshot | Deep offsets process skipped rows; a changed project revision restarts browsing | Frequent edits interrupt traversal; keep unique ordering and explicit history_changed errors rather than unexplained duplicates/gaps |
| B — keyset pages with revision guard | Bounded deep-page work; exact traversal while the revision stays unchanged; no retained snapshot | No arbitrary page-number jump; changes require refreshing the list | Even unrelated project edits can invalidate a cursor; preserve mixed date-DESC/ID-ASC order, bind context and recheck access on every page |
| C — materialized history snapshot with TTL | Stable browsing despite insertions/deletions while users navigate | Snapshot storage, quotas, expiry, cleanup and revocation handling | Temporarily retains deleted sensitive content; resource/privacy boundaries and recovery require additional design |

Accepted choice: **B** for a minimal compatible repair. This replaces the initial
unselected watermark proposal with an explicit revision guard; do not pretend a
random-ID watermark is a snapshot. No new isolation policy is chosen by it.

The user accepted the shared packet in the selection question:

- New paged envelope resource; retain old array consumers and add explicit legacy
  truncation/paging metadata, with no scheduled compatibility removal in this WP.
- Default page 100, maximum 500; signed opaque context/cursor expires after
  15 minutes and binds project, principal, filters, ordering and revision. Every
  page rechecks current authorization. No server transaction held across browsing.
- Filter by task/employee/date on the server before pagination. Milestones show
  their own recorded entries plus descendants' history (root-inclusive subtree);
  never hide pre-existing direct milestone records. Identify source tasks and keep recorded
  totals independent of page count. Show legacy minutes separately; do not change
  existing actual/summary billing semantics to make history totals appear equal.
- Full history export uses a separate schema/download, with HTML/CSV/JSON and
  browser PDF like the existing export workflow. Importable Project JSON remains
  the compatible project schema with no embedded time facts; additive timezone
  provenance must not weaken its identity/import protections. Preserve manager/
  export-scope checks, escaping and formula protection.
  Export consistency/resource bounds must be frozen in the implementation plan;
  never hold a transaction across a slow download or silently truncate a file.

## D06 — exactly three timezone owners

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — one configurable installation IANA planning zone | Smallest coherent change; all projects and common daily ceilings use the same civil day | Teams share one planning clock | Pin compatible defaults to Europe/Moscow; validate zone/tzdata, expose the zone and make changes explicit rather than inheriting host/browser settings |
| B — IANA zone per project | Project-local deadlines/windows | The current date-only capacity ledger cannot prove a common day across mixed-zone projects | A bounded first version must disallow sharing an employee across different project zones; supporting that sharing requires a further agreed accounting-day/interval design |
| C — IANA zone per employee for scheduling/capacity | Common limit follows each employee's local day across projects | Multi-person tasks, project calendars and exception dates need conversions | Largest model change: allocation across midnight/DST cannot be inferred from date+minutes alone; requires a broader explicit scheduling contract |

Accepted choice after code inspection: **A** for this bounded repair. The original
D06 B recommendation was unapproved and depended on unresolved common-day rules.
If B/C is chosen, resolve the stated extra restrictions/conversion contracts before
implementation; do not silently change accepted employee-allocation semantics.

For A, use an installation configuration setting with a compatible Moscow
default, checked at startup and carried in relevant backup/export metadata. Use
the installation zone for planning today, overdue/default dates, reports and
labelled UI timestamp presentation; storage/API instants and security durations
remain independent. Imports with a different declared planning zone require
explicit acknowledgement, preserving date labels rather than converting them.
Legacy files without metadata must be labelled as unproven provenance; former
releases used Moscow, but the file alone does not prove its origin. A zone change must be explicit and documented; no
historical DATE or effort values are rewritten. Planned minutes remain nominal window minutes. UTC audit instants,
TOTP/session expiry and elapsed security durations remain independent. Nonexistent/
ambiguous wall-clock windows must be detected before any future calendar publication,
with a visible warning instead of a guessed instant. Keep the live calendar module
disabled; provider timezone identifiers require validation, not blind string replacement.

## D12 — Project JSON provenance compatibility (pending)

Strict old importers reject unknown fields, so arbitrary embedded timezone metadata
is not forward compatible. Legacy-file support and old applications consuming new
files are different guarantees. All options keep the existing legacy HTTP export
contract and import identity protections; a new format must use an explicit resource/
format rather than silently replacing the v1 response.

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — retain the current JSON default; add an optional portable JSON with a versioned metadata envelope | Existing file/API workflows remain unchanged; users can explicitly carry the planning zone | Two clearly labelled JSON choices | Legacy files cannot prove their zone; imports must show that uncertainty instead of fabricating provenance |
| B — make portable versioned JSON the UI default; retain legacy JSON through an explicit compatible choice/API | Normal new exchanges carry zone metadata | Old applications cannot read the new default file until updated or given the legacy format | Explain forward incompatibility clearly; new importer must continue accepting old files without changing dates/identity rules |
| C — ZIP with unchanged project.json and separate metadata.json | Payload stays familiar; metadata travels alongside it | New archive import/export path and extraction/size checks | Additional parser, decompression/path limits and cleanup/security tests are required |

Recommendation: **A** for the smallest compatibility change. The user has not
selected D12 yet. No option may embed time facts into ordinary Project JSON.

## Primary sources checked on 2026-10-30

- [PostgreSQL LIMIT/OFFSET](https://www.postgresql.org/docs/current/queries-limit.html)
  and [transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html).
- [PostgreSQL date/time types](https://www.postgresql.org/docs/current/datatype-datetime.html).
- [Go time and locations](https://pkg.go.dev/time#LoadLocation), including Date and ParseInLocation.
- [Browser Intl.DateTimeFormat](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Intl/DateTimeFormat).
- [Microsoft provider timezone discovery](https://learn.microsoft.com/en-us/graph/api/outlookuser-supportedtimezones?view=graph-rest-1.0).

These references inform the alternatives. They do not approve a design or prove
that the proposed migration, browser/DST cases or complete exports have been tested.
