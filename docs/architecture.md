# Architecture and decisions

gmVector is a self-hosted application: React/TypeScript in the browser, a Go HTTP
API and worker, PostgreSQL, and an nginx HTTPS reverse proxy. Docker Compose keeps
state in persistent volumes and secrets in a separate protected directory.

## Trust boundaries

The reverse proxy overwrites the trusted client-address header. The API accepts
that header only from explicitly trusted proxy addresses. Browser session cookies,
CSRF tokens, exact Host/Origin checks, TOTP, password hashing, role checks, scoped
service tokens and optimistic project revisions remain enforced server-side.
Commercial activation and remote entitlement services are absent.

The database has separate bootstrap, migrator and runtime roles. Runtime cannot
create databases, roles or schema objects. The installer performs role migration
before schema migration; role ownership is tied to a local installation identity.
Images run unprivileged with read-only roots, dropped capabilities and no-new-privileges.
Proxy certificate keys are mounted read-only into the proxy only.

## Domain and scheduling

Projects contain hierarchical tasks, dependency links, participation schedules,
and planned/actual effort. A task with children is a summary task. Dependencies
support together, after and delayed scheduling. Cycles are rejected.

Employee availability combines per-project weekday/date windows and quotas with
a global daily ceiling. Quota is consumed by actual scheduled work, not reserved
on empty days. Insufficient capacity extends task duration; conflicting total
allocations block publication of affected calculated dates until corrected.
Work calendars pause execution on nonworking days in both duration display modes.

Projects select immutable annual calendar versions, retaining a compatible anchor.
Unselected years use visibly provisional weekdays; invalid selected versions never
silently fall back. Multi-year calculations share an explicit cancellable work
budget, including calendar validation and related-project capacity checks. The
date domain remains 2000–2200; derived-ledger limits replace the old 730-day guard.
Snapshots store authoritative inputs rather than large recomputable load arrays.
See [WP-01 contract and evidence](remediation/2026-10-30/wp-01-verification.md)
for verified scope and outstanding acceptance boundaries.

## Accounts and integrations

First administrator setup opens at the normal URL without an installation key,
then requires TOTP confirmation. Setup is permanently closed after completion;
see [first-run setup](first-run.md).
Recovery is a privileged local operation and revokes prior credentials. Corporate
LDAP uses verified TLS and stable IDs; explicit disabled state revokes access,
missing search results only require review. Local administrators remain independent.

Service-desk integration is external REST/OpenAPI code, not a built-in provider.
Directory records preserve provider plus immutable external identity; API cannot
write the reserved LDAP namespace. Email invitations and customer messages use an
encrypted outbox. Calendar provider code is retained but live calendar OAuth/sync
is not enabled in this release.

## Data and privacy

Task/project data stays in the configured installation. Diagnostic metadata is
bounded and excludes raw URLs, bodies and authentication headers. Optional Bug
Report storage accepts encrypted envelopes under the operator's public support
key. The separate GitHub action opens only a user-reviewed public Issue draft;
it never attaches diagnostics or submits an Issue automatically.

## Distribution

Apache-2.0 applies to the project's own code. Official release signatures can
identify publisher artifacts, but a community source build needs no publisher key.
The public repository starts from a reviewed clean snapshot. Old private Git history,
customer projects, populated deployment settings, keys and archived packages are
not part of it. Read/clone/fork access is public; official-repository writes are
limited to maintainers.

## Language

English is the interface baseline with Russian, German, French, Spanish, Brazilian
Portuguese and Simplified Chinese catalogs. Profile preferences persist server-side;
a prelogin picker is local. User-generated task text is never machine-translated
or uploaded. See [localization](localization.md) for exact coverage and limitations.
