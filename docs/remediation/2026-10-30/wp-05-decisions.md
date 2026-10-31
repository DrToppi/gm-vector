# WP-05 admission decisions

Status: **PROPOSED — HUMAN SELECTION REQUIRED**. Checked on 2026-10-31.
WP-03 corrected candidate is 9b03c90; its final CI/acceptance remains separate.
This packet does not authorize a deployment, a policy exception, new backup
architecture or use of real integrations. Read [the matrix](wp-03-05.md),
[decision register](decisions.md) and [workflow](../../development-workflow.md).

## D09 — required browser coverage

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — pinned Playwright Chromium/Firefox/WebKit critical scenarios, keeping existing CDP checks | Shared scenario/fixture API across three engines; local and CI reproducibility | Additional development dependency, browser downloads and native libraries | Version/browser coupling and engine differences require explicit maintenance; WebKit is not proof of actual Safari/iOS coverage |
| B — separate WebDriver harnesses | Standards-based control and independent driver choices | Duplicate driver, lifecycle and fixture plumbing | Different wait/error semantics can hide missing scenarios or introduce flaky results |
| C — external browser service | Wider actual OS/browser combinations without local infrastructure | Service cost, credentials and internet availability | Test traces/artifacts cross a new trust boundary; synthetic-only data and service authorization are needed |

Recommendation **A**, not approval. Freeze the exact package/browser revisions,
supported Linux environment and native dependencies after selection. Current
public metadata suggests @playwright/test 1.64.1 (Apache-2.0, Node >=20); it is not
installed or accepted by this packet. Verify the actual resolved lock and peers.
Each final engine must run the same critical real API/PostgreSQL paths: first
setup/TOTP/invitation/login/logout; project/task/participation/calendar/capacity;
ordering/session races; history/exports; dialogs/keyboard; Gantt/map interactions;
Bug Report privacy. Explicitly test engine-specific print/download behaviour;
do not silently skip a missing engine or disable TLS checks for real endpoints.

## D10 — vulnerability and release gate

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — block every HIGH/CRITICAL finding, including unfixed | Simple, conservative release rule | Upstream defects can block releases indefinitely | Operational pressure may move outside the documented process; no implementer waiver is permitted |
| B — same default block, with narrowly scoped, human-approved expiring exceptions | Explicit risk ownership, remediation date and audit trail | Exception records and expiration/recheck need maintenance | Overbroad or stale approvals weaken the gate unless enforcement fails closed |
| C — block only findings classified reachable | Less noise from unused packages | Requires trustworthy reachability evidence for every platform/artifact | Incomplete analysis can miss native/dynamic paths; not equivalent to absence of exploitable defects |

Recommendation **B**, not approval. Proposed B policy: exceptions last at most
14 calendar days and require the human owner to approve advisory, package/version,
exact image digest/path, rationale, compensating controls and expiry/recheck.
Implementers cannot approve their own exception. No automatic ignore-unfixed or
reachability waiver; retain complete unfiltered reports. The known unimported
OpenPGP advisory is not automatically exempted by this packet.

Regardless of choice, scan the actual app/proxy/PostgreSQL image digests, not just
mutable tag names; retain scanner version, advisory DB timestamp and SBOM/report
membership for all three. Missing image/report, scanner failure, malformed output
or advisory data older than the proposed 24-hour maximum blocks the gate. Apply
the selected gate on each required candidate CI run; scheduled-only checks cannot
accept a different SHA. All choices require unfixed-finding, unavailable/stale DB
and omitted-image controls under their selected policy. B additionally requires
missing/expired/overbroad exception failures; A must not admit exceptions, and C
must reject missing/invalid reachability classification instead of calling it safe.

Current fact: CI enforces HIGH/CRITICAL only for the proxy. scan-images.py scans
three tags but prints counts without making those findings fail the command.
This packet is not evidence that all-image enforcement already exists.

## D13 — recovery scope, upgrade origins and reference objective

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — retain matching format-2 DB/config backup, test separate operator-managed installation artifacts; 30-minute reference recovery to the last verified snapshot | Smallest architecture change; validates the existing backup contract and real usable recovery | Reports, TLS and deployment artifacts need a separate matched operator copy/procedure | Later writes may be lost; a DB/config archive alone cannot recover every installation component |
| B — extend the encrypted versioned archive to reports/TLS/deployment artifacts, with the same proposed reference objective | More self-contained disaster recovery | New archive version, parsers, membership/resource/compatibility rules and file ownership handling | Sensitive private material and inconsistent volume/config snapshots need additional controls; old-format compatibility must be explicit |
| C — physical backup plus WAL/PITR, proposed recovery point within 15 minutes | Smaller potential data-loss interval | New storage/retention/replication/monitoring and recovery architecture | Missing WAL, key loss and archive consistency can defeat recovery; requires another bounded design before implementation |

Recommendation **A**, not approval. Proposed 30-minute clock starts when the
prepared operator begins the documented recovery action and ends only after real
password/TOTP login, intended privileges, project read/write/export and restart
are usable on the reference fixture. Record all preparation assumptions, elapsed
time, snapshot timestamp and last recovered acknowledged write. Recoverable data
ends at that verified matching snapshot; no daily backup cadence or fixed 24-hour
data-loss guarantee is invented. This is a reference target, not a universal SLA.
For C, the proposed 15 minutes is a recovery-point/data-loss objective, not a
recovery-time objective; C's recovery-time target needs its new bounded design.

Use the established synthetic scale: five projects × 300 tasks, 30 employees and
at least 1001 facts, multi-year calendars, real synthetic encrypted TOTP/invitations/
outbox/directory records. Restore with outbound integrations isolated. Measure
the full procedure, not pg_restore alone. Test rollback after controlled failed
upgrade with a matching application/configuration/snapshot; image-only downgrade
is insufficient.

Fresh distinct-origin fixtures are required for these explicit public commits:

| Origin | Allowed admission path to the final candidate |
| --- | --- |
| Public main ab4abfe | Operator-assisted upgrade; no installed updater is assumed |
| Working updater c8c34ae | Operator-assisted upgrade; keep app-only infrastructure guard |
| Accepted WP-04 e97971d | App-only updater eligible only while its infrastructure contract remains identical |

Code verification: update_engine.py compares six infrastructure paths byte-for-
byte. The c8→WP-04 change to install.py's planning_timezone default changes that
contract. The existing app-only path must continue to return
infrastructure_change_requires_operator; do not weaken the guard or silently add
host-service self-upgrade. Historical c8→WP-01 evidence and current same-candidate
CI recovery do not prove all three fresh origins into the final candidate.

Format 2 currently includes a logical DB dump and fixed allowlisted configuration,
application key, DB password, instance ID and optional LDAP/SMTP/updater files/CAs
and report public key. It does not include report volume files, TLS private keys/
certificates, proxy config, updater journal, deployment/source/image artifacts or
the separate backup decryption/support private keys. Under A preserve and test
those separate operator-managed recovery steps; document each persistent volume
and retain private keys outside distributable artifacts. Logical dumps also omit
cluster-global roles: recreate and verify bootstrap/migrator/runtime privileges.

Existing rich restore proves 1001 facts/configuration and least-privilege API use,
but seeds password_hash='unused', invalid TOTP bytes and a restored session. It
does not prove restored password/TOTP, invitations or encrypted outbox usability.
Fill those gaps with real synthetic credentials and isolated fake integrations.
Also test online backup coherence: pg_dump and manifest counts currently execute
separately. Stopped-writer updater snapshots avoid that specific race; an online
consistency fix needs an actual controlled failure before implementation.

## Remaining package gates

After human selection and WP-03 acceptance, freeze a separate bounded WP-05 plan,
file ownership, fixture matrix, exact tools and evidence protocol before code.
Retain the accepted 60-minute final soak with the established workload and SLOs;
freeze the exact phase distribution without shortening that observation. Expand
all-image/security/privacy/backup/upgrade failure coverage against the final SHA,
then obtain a different READ-ONLY review. No operational acceptance or deployment
is implied by selecting these proposals.

## Checked primary references

- [Playwright browser/version coupling](https://playwright.dev/docs/browsers).
- [Playwright supported environments](https://playwright.dev/docs/intro).
- [Trivy filtering and expiration](https://trivy.dev/docs/latest/configuration/filtering/).
- [Trivy image scanning](https://trivy.dev/docs/latest/scanner/vulnerability/).
- [PostgreSQL18 logical dump boundaries](https://www.postgresql.org/docs/18/app-pgdump.html).
- [PostgreSQL18 PITR](https://www.postgresql.org/docs/18/continuous-archiving.html).
- [Recovery time objective](https://csrc.nist.gov/glossary/term/recovery_time_objective)
  and [recovery point objective](https://csrc.nist.gov/glossary/term/recovery_point_objective).

These sources justify tool/recovery distinctions; they do not select a policy or
establish a passing test. The three recommendations remain unapproved until the
user explicitly chooses them.
