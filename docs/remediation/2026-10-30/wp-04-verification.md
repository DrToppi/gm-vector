# WP-04 verification record

Status: **PASS — INDEPENDENTLY VERIFIED** at
`e97971ddf5125b433f27e194b1be3144c3748d67` on 2026-10-30.
Baseline: `d656d65`, containing independently accepted WP-02 implementation
`400ae188b6c059170dacfba7b3c8aec4a8af13af` and its documentation closure.
Task branch: `feature/history-and-timezones`.

The [bounded plan](wp-04-implementation.md), accepted
[D05 B/D06 A/D12 A](wp-04-decisions.md) and matrix 04-01–09 govern this package.
Intermediate checkpoints/failures below are historical; final acceptance and its
limits are recorded at the end.
Main and the running updater installation remain outside this work.

## Independent plan gate

A separate READ-ONLY reviewer checked accepted choices against current code and
found no architectural blocker. Before coding, the plan was refined to retain
NULL-employee facts, define raw distinct-task legacy sums, bound the export slot
through network-buffer lifetime, and specify zone-acknowledgement preview behaviour.
Browser output conversion and PDF DOM construction are bounded separately. Test
refinements include an internal DST transition with valid endpoints and unsupported
browser timezone data. This permits bounded implementation, not a PASS verdict.

## Demonstrated baseline failures

Before production changes at plan source efe1751, the writers actually executed:

| Regression | Observed baseline | Result |
| --- | --- | --- |
| TestTimeHistoryLegacyMetadataRegression | 3000 facts silently reduced to 1000, total/truncated headers absent | RED, assertion failure |
| TestPlanningTimezoneProviderRegression | A New_York busy block was published as Russian Standard Time | RED, fake transport assertion failure |
| Real React old_task browser case | Older task record beyond the project's first 1000 missing | RED |
| Real React milestone browser case | Descendant records excluded by exact-task filtering | RED |
| Real React planning_zone browser case | Fixed expected New_York 2026-10-29 produced Moscow 2026-10-30 | RED |
| Real React portable browser case | Metadata envelope went through strict legacy import route | RED |

These were actual assertions in the existing components/backend, not missing
symbols or compilation failures. No live Graph/directory/mail system was called.

## Installer and recovery fixture checkpoint

New-install configuration now explicitly records the compatible Europe/Moscow
default. The existing installer suite first failed at the missing field, then all
five tests passed after the narrow initializer change. This is installer component
evidence, not timezone/history acceptance. Python syntax compilation also passed.

The recovery fixture now prepares 1001 old leap-day facts, including a NULL employee
and a removed assignment, root/child raw legacy minutes, a private New_York config
and digest assertions. Its first execution used the previously built a2e07a binary:
restore reached the expected missing /auth/me timezone assertion. That old-binary
failure is not claimed as an exact-current-source regression. At that checkpoint,
actual restored API/history/config and packaged-image tzdata checks remained open;
their successful final results are below. The backup mechanism was not rewritten.

## Actual packaged-export RED

The root built a separate image from committed backend8743063 without replacing
the working image tag. A fresh disposable Docker installation completed real
HTTPS/admin/TOTP setup and accepted its New_York metadata. The new10000×4000-byte
comment export probe then observed HTTP200 on a second export while the first
TLS client's receive window/body was held. The busy guard assertion failed;
no later zone/large-body success is claimed from that run. Fixture volumes and
containers were removed by finally-cleanup.

This confirms an end-to-end lifetime mismatch through the proxy. It does not
prove tmpfs exhaustion. The bounded plan now specifies per-response nginx
buffering control; a corrected actual-image replay remains required. This image's
web bundle was a development snapshot, so the probe is API/proxy evidence only,
not a final frontend or exact-final-release acceptance.

## Executed focused checks

The following component runs actually passed before the final combined freeze.
They do not substitute for final exact-SHA CI and independent acceptance.

| Check | Executed outcome |
| --- | --- |
| Pagination with race instrumentation | PASS 29.461s ; 0/1/999/1000/1001/10000, exact tied-date ID traversal, NULL employee, legacy headers, inclusive subtree, removed assignments and 1999 facts |
| Current authorization and cursor revision | PASS 3.982s; viewers, principal/auth-kind/filter binding, expiry/tampering, revocation, NULL ownership and revision invalidation |
| Strict portable/legacy project formats | PASS 2.826s; preview/apply source-target acknowledgement, unknown/version/size failures, no fabricated identities/facts |
| Installation clock integration | PASS 2.961s; four App zones on one Store, direct Import clock binding, related-project reuse, unchanged dates and audit instants |
| Export boundary units | PASS 1.024s; exact encoded length, overflow without mutation and legal 4000-byte control-text expansion |
| Export integration with race instrumentation | PASS 44.842s; complete 10000 rows, actual 100001 rejection/100000 boundary, actual escaped 64MiB rejection, observed database lock wait with an actual API fact writer, held output/slot, cancellation and header/row deadlines |
| Installer defaults | Five tests PASS; explicit compatible initialization |
| Runner acceptance guards | Ten tests PASS; focused mode cannot masquerade as all, production DB refused, skipped roots/required children rejected and private logs cannot overwrite existing files |
| Frontend checkpoints513edc3/7fcfbdc | Build/TypeScript/seven locales and pure checks PASS;20 asserted browser scenarios plus optional screenshot artifact case; former-ID absent from catalog demonstrated RED→GREEN and independently reproduced |
| Expanded security matrix | PASS33.691s;448 cells covering59 routes/seven roles and 35 explicit token/CSRF/strict-body/no-side-effect variants |

The deliberately cancelled export construction emits the existing generic
operation_failed diagnostic. This is a controlled cancellation, not evidence
resolving the unrelated older diagnostic investigation reserved for WP-05.

## Packaged backpressure control and successful replay

After the narrow response-header correction, the first replay passed both busy
checks and concurrent read/write controls, then timed out while draining with an
artificially shrunken TCP receive buffer. That run is retained as a failed fixture
control, not a full export pass. A one-variable replay used the same 99a102c image
and ordinary receive window, still leaving the body unread during both busy checks.
No application timeout or resource limit was increased.

The replay completed a 41660487-byte JSON document with exact 10000 IDs, dates,
minutes and legal 4000-byte comments. Both concurrent export requests returned 429;
ordinary project GET and an exclusive capacity edit completed before body draining.
The actual API enforced 805306368-byte memory limit, remained running and recorded
no OOM/restart. This is a successful operation under the container limit, not a
continuous memory-peak measurement or proof for every dataset/network.

The same disposable installation passed actual packaged UTC/Moscow/New_York/
Kathmandu configuration, nonroot image identity and observed stable API/worker
startup, same-service valid/invalid/valid controls, omitted-setting compatibility,
repeated preparation, unchanged secrets, real TOTP and credential-revoking recovery.
At that checkpoint, later runtime hardening and the final frontend still required
final-image CI; this is complete in the final record below.

## Intermediate combined-gate failures

The first broad integration invocation stopped because native nginx was not on
PATH; completed API/diagnostic checks are not called a complete suite. The corrected
runner accepts an explicit `--nginx-bin` and keeps that failed prerequisite record.
The complete suite was not accepted from that partial run. No main merge or
deployment occurred.

The next broad server run passed the proxy prerequisite but stopped at the old
security inventory guard (new59 routes). The owner added explicit route/role and
denied-state coverage rather than only changing a count; its focused448-cell
matrix then passed. A new READ-ONLY review regression also actually demonstrated
that malformed raw query encoding was being dropped: both history routes returned
HTTP200/all3 records for a broken employee filter instead of422. Valid encoded
filter controls returned2 records. The plan now requires strict raw-query parsing;
no role/scope escalation is claimed. Its corrected run and the combined final gate
were still pending at that checkpoint; final results follow.

## Acceptance checklist

- Reproduce project-wide 1000-row truncation hiding an older task and a mismatched
  planning day with fixed UTC instants before changing production code.
- Record exact fixtures, IDs/counts/digests, page traversal, independent expected
  totals, cursor/access failures and unchanged legacy array/JSON contracts.
- Prove revision invalidation and coherent exports using actual database barriers;
  include removal of assignees, milestone-own facts and retained old dates.
- Execute complete HTML/CSV/JSON/browser-PDF history paths, formula/XSS fixtures,
  ordinary 10000-row exports and actual row/byte/concurrency/cancellation guards.
- Record export memory observations and normal-read behaviour while exporting;
  do not confuse an engineering guard with a business/history retention limit.
- Verify the four-zone clock table, midnight/leap/year/DST cases, packaged tzdata,
  disabled/fake provider behaviour, portable mismatch acknowledgement and no
  historical date/minute or security-expiry rewrite.
- Run all seven locale checks, affected frontend browser regressions and the
  required prior API/security/recovery suites. Skips are not evidence of a pass.
- Obtain independent READ-ONLY review and successful final exact-SHA CI. Only then
  record `PASS — INDEPENDENTLY VERIFIED`, with limitations stated explicitly.

## Final exact-source acceptance

The source tree was clean at full-run launch on e97971d. The following completed
against that implementation, with synthetic identities, projects and integrations:

| Gate | Actual final result |
| --- | --- |
| Unified local unit/race/API/security/recovery/browser runner | PASS, exit 0; all required named roots and children executed without accepted skips |
| Security matrix | 448 cells passed ; 59 routes, seven roles and 35 explicit boundary variants |
| History/clock/browser regression | 20 asserted scenarios passed; optional screenshot artifact case is not counted as another functional assertion |
| Rich matched backup/configuration restore | PASS; 1001 retained facts, full-row/date digest, NULL employee, raw legacy 40 minutes, New_York policy and actual least-privilege API |
| Real API/PostgreSQL browser E2E | PASS 20.94s locally; 20.81s in CI, including existing identity/scheduling/export/recovery protections |
| Final local packaged image | PASS; matching e979 release metadata, nonroot UID 65532, 10000 exact rows / 41660487 encoded bytes, two observed busy 429 responses, concurrent read/exclusive capacity write, enforced 768MiB container without OOM/restart |
| Packaged planning policy | UTC/Moscow/New_York/Kathmandu, rejected invalid setting, compatible omission, stable observed API/worker startup, repeated prepare and credential-revoking recovery passed |
| Exact-source [CI 41066617244](https://github.com/DrToppi/gm-vector/actions/runs/41066617244) | SUCCESS; full runner, OpenAPI consistency, npm audit, govulncheck reachability gate, builds, proxy scan, actual HTTPS/image installation and host updater/migration recovery |

CI separately completed its 41660488-byte packaged export with the same data
oracles. Generated timestamp metadata may differ; byte-identical downloads or
independently built image digests are not claimed. The final local image was
`gm-vector:wp04-e97971d`; it did not replace the running image tag or installation.

The different READ-ONLY reviewer checked final requirements/code/evidence and
independently executed RawQuery/race, the eight export/race scenarios, pure
clock/export checks and the privacy/former-employee browser regressions. They
queried exact-source CI independently, inspected restore/image receipts and
confirmed no remaining WP-04 blocker. Verdict: **PASS — INDEPENDENTLY VERIFIED**.

Private raw evidence SHA-256 (raw artifacts stay outside Git):

- Local full runner: `ac6e9d9a985ae09767d61c88f04b6c2a7c226905fb7653ef47abe7fda13506eb`.
- Final packaged fixture: `8fb683d5a6c89faf12c5e47d722f2f43a84116222ad8d8ff5670bd4e72a8b81a`.
- Independent export: `49a035b034f665c53fd479fcc5e4d0ec667af09fa8b69f7e9795b095d96939cc`.
- Independent malformed-query regression: `a7e331249e4aa9a6e333f9a56b1d210ae85f431b3ca22b5e26eb28e2adee7d3f`.

Limits: no live calendar/provider consent is enabled or proven. Future timezone
discovery needs the delegated permission review described in
[integrations](../../integrations.md). Resource guards and successful datasets do
not prove a universal memory peak or completion on arbitrarily slow networks.
Historical opt-in profiling skips are not counted as completed performance gates.
The existing large-bundle warning belongs to WP-03. CI govulncheck reported zero
reachable/imported-package vulnerabilities but one advisory in a required module;
it did not identify the module in default output. Do not call all required modules
vulnerability-free; detailed follow-up belongs to WP-05. Final three-engine,
all-image, full recovery and 60-minute operational acceptance remain open.
No main merge, release publication or working-instance deployment was performed.
