# WP-05 bounded implementation and evidence plan

Status: **PHASE-1 PLAN ADMITTED BY INDEPENDENT READ-ONLY REVIEW ON 2026-10-31**.
No implementation result is implied. At admission no WP-05 code or dependencies
had been installed. Exception admission remains held as described below.
Baseline runtime: accepted WP-03 9b03c90, documentation closure f516d48.
Branch: feature/operational-hardening. User choices D09 A/D10 B/D13 A are recorded
in [the accepted packet](wp-05-decisions.md); [D14](wp-05-approval-proof.md) is
unselected. This plan applies the full [05-01–17 matrix](wp-03-05.md).

## Admission and exclusions

After independent READ-ONLY plan review, phase 1 may implement strict/no-waiver
image gates, browser fixtures, recovery controls and their scoped corrections.
Exception admission/configuration is a separate held phase until D14 is selected
and its exact trust/artifact verification is reviewed. No individual waiver exists.
The package cannot be accepted with an unimplemented selected approval path.

The reviewer independently checked accepted choices, current source, exact pins,
named mandatory roots, UNKNOWN handling, secret-volume mapping and primary
recovery scope. Admission allows implementation only; it is not final acceptance.

Preserve API/auth/session/TOTP replay/TTL/role/CSRF/proxy/TLS boundaries, calendar
and history contracts, the six-file updater infrastructure guard and archive format2.
No main merge, application deployment, release publication, host monitoring,
real email/directory/calendar call, archivev3, PITR or host-agent self-upgrade.
Changes outside the permitted areas require an actual finding and recorded scope
review first; material contract changes still belong to the human.

## Ownership and permitted modules

| Writer | Permitted area |
| --- | --- |
| Root/orchestrator | scan-images.py and new scanner/privacy gate helpers/unit fixtures; verify.py and test_verify.py; production.yml/required CI; final load-profile/runner extension; documents/evidence |
| Browser writer | web package/lock and explicit browser test/config typecheck; playwright.config.ts; browser fixture/spec/reporter files; new Playwright real-server Go test fixture. Coordinate any extraction of existing e2e_test.go setup; retain every CDP assertion |
| Recovery writer | backup.py; test_backup_manifest.py/test_backup_coherence.py; backup-coherence-integration-check.py; recovery-integration-check.py/recovery_fixture.py; operator-upgrade-integration-check.py. No updater/installer/compose/runtime edits without reproduced need and coordination |
| Independent reviewer | READ-ONLY plan/code/contracts/evidence review; independent isolated checks only after coordinating CPU/DB/Chrome/probe resources |

Keep one writer per file. Root alone stages shared runner/CI changes. Separate
coherent code/test fixes, dependency additions and evidence commits. No writer
may use another's in-progress files as a passing final SHA.

## Frozen entry points and required receipt membership

These are planned commands, not claims that the new files or switches exist yet.
The existing `verify.py unit/integration/e2e/all` commands retain their scope and
disposable-DB guard. Add the named new checks to the appropriate complete suite;
focused switches must identify themselves as partial verification.

| Entry point | Required membership / command contract |
| --- | --- |
| `scan-images.py` | Required `--app-image`, `--proxy-image`, `--postgres-image`, full `--source-revision` and new private `--output-dir`; optional explicit `--docker-host`. Resolve all three before scanning. `image_gate.py` validates the complete manifest/reports; `test_image_gate.py` tests fail-closed admission. Labels are exactly app/proxy/postgres. No implicit working-image tags or approval flags |
| `artifact-privacy-check.py` | Required clean `--source`, `--source-revision`, scanner `--scan-directory` and new private `--output-dir`; use `artifact_privacy.py` and `test_artifact_privacy.py`. Required roots P01 full reachable Git history, P02 tracked source, P03 exact build context, P04 source/package archive, P05 every image layer, P06 generated marker controls. Shallow history or a missing root blocks admission |
| `verify.py e2e --only-playwright` | Build web and run exactly `TestPlaywrightE2E` with `GMA_PLAYWRIGHT_E2E=1`; explicitly partial, never accepted with another suite. Complete e2e/all retain CDP and add this root with fixed chromium/firefox/webkit subtests. Require each B01–B09 receipt for every engine, no retries/skips. `TestPlaywrightHarnessValidation` is a no-DB unit guard |
| Browser npm commands | `test:browsers` invokes `playwright test --config=playwright.config.ts` and refuses missing Go-owned private fixture IPC; `check:browsers` invokes `tsc --project tsconfig.browser.json`, covering browser/**/*.ts and playwright.config.ts. Keep component discovery/check:tests unchanged and include new authored sources in lint/format |
| `backup-coherence-integration-check.py` | Optional new exclusive private `--evidence-file`; no host config/DSN/URL/skip flags. Creates isolated PostgreSQL. Required C01 online dump/manifest snapshot, C02 actual snapshot-owner termination cleanup, C03 deterministic timeout fault after a real snapshot starts. Timeout injection is labelled, not claimed as a natural pg_dump timeout |
| `recovery-integration-check.py` | Optional `--candidate-source` defaults to repo; app/db/proxy image overrides resolve and freeze matching immutable IDs; optional new private evidence file. No implicit build/pull/retag. Required R01 matching identity/roles/data/restart, R02 wrong archive key, R03 tamper/truncation, R04 valid archive with wrong application key, R05 missing/unsafe dependencies, R06 staged/active mismatch, R07 operator artifacts, R08 no outbound integration |
| `operator-upgrade-integration-check.py` | Required allowed `--origin-source`; optional candidate source and six explicit origin/candidate app/db/proxy image overrides; optional private evidence file. Required U01 real original identity/seed, U02 six-file admission, U03 forward migration, U04 failed candidate with matching rollback, U05 recovered identity/data/rights/restart, U06 RTO/RPO/operator artifacts. Run separately for each accepted origin. Successful and failed branches start from independently fresh original installs |
| `final-load-check.py` | Required disposable `--dsn-file` and new private `--evidence-file`; records clean full HEAD and runs exact `TestReadPathLoad` with `GMA_READ_PATH_LOAD_PROFILE=operational`, 600/3600 seconds. Explicit long timeout preserves the old runner's 600-second bounds. No arbitrary shorter-duration flags. `test_final_load.py` checks invocation and refusal controls without a DB |

CI/reference invocations pass all image IDs explicitly and verify source/image
identity; defaults cannot accept a stale package. Root wires coherence and the
primary recovery command into required integration/CI and three origin commands
after candidate images exist. Also require existing `TestSecurityAuditResilience`
and `TestSecurityAuditRevocationSeatRace` explicitly: their mere existence in the
tree does not make the present `verify.py all` execute their opt-in DB bodies.

Root scanner/privacy helpers use Python stdlib, existing pinned Trivy **0.76.1**,
and Gitleaks **8.30.1**. The read-only local Linux x86-64 binary fingerprint is
`88f91962aa2f93ac6ab281d553b9e125f5197bbbce38f9f2437f7299c32e5509`;
CI must verify its release artifact and actual version, not install a moving tag.
Use explicit default secret rules, ignore inline allow comments and disable
caller-provided ignore/config/baseline filters. Gitleaks and custom marker/path
checks are detection controls, not proof that every possible secret is detectable.

Checked primary references: [pinned Playwright browser manifest](https://raw.githubusercontent.com/microsoft/playwright/v1.64.1/packages/playwright-core/browsers.json),
[native dependencies](https://raw.githubusercontent.com/microsoft/playwright/v1.64.1/packages/playwright-core/src/server/registry/nativeDeps.ts),
[Gitleaks 8.30.1](https://github.com/gitleaks/gitleaks/tree/v8.30.1),
[Trivy filtering](https://trivy.dev/docs/latest/configuration/filtering/),
[advisory metadata](https://github.com/aquasecurity/trivy-db/blob/main/pkg/metadata/metadata.go),
[pg_dump snapshot](https://www.postgresql.org/docs/18/app-pgdump.html) and
[snapshot synchronization](https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-SNAPSHOT-SYNCHRONIZATION).

## P1 — immutable all-image/security gate (05-01–04)

Retain the existing pinned Trivy digest
790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c unless an actual
supported-tool defect requires a reviewed change. Record its actual version.
Use isolated candidate image labels; never retag/replace the working local image.

- Resolve all three app/proxy/PostgreSQL image IDs before export/scan, save by ID,
  and name local config image ID versus optional registry manifest digest precisely.
  Verify saved config/layer identity and stable archive hashes.
- Retain complete all-severity/unfixed raw JSON and CycloneDX SBOMs; validate
  required identity/membership against frozen images. Missing/empty/unsupported or
  swapped/truncated/malformed reports cannot mean clean. Bind output hashes and
  scanner/advisory metadata in a manifest for exactly all three labels.
- Run the pinned producer in an isolated directory with explicit empty ignore/
  config files and no inherited TRIVY_* filters, VEX or ignore-unfixed settings.
  Mount input read-only, outputs/cache separately; do not mount a Docker socket
  into the scanner. Confirm inputs remain unchanged.
- Gate HIGH/CRITICAL. UNKNOWN severity makes admission non-PASS pending explicit
  classification/disposition, not silently safe; missing/invalid severity is malformed.
  No nonempty exception set is admitted before the selected D14 verifier exists.
- Validate advisory DB UpdatedAt, not cache mtime/DownloadedAt. Reject absent,
  malformed, future or >24h-old metadata, scanner/process failures and omission.
  Keep the report/DB epoch coherent and do not label an offline/stale scan PASS.
- Inert fault fixtures cover every failure above, unapproved filtering, an
  unfixed blocking finding, wrong identity and (after D14) invalid/scope/expiry/
  approval/replacement cases. Never ship a deliberately vulnerable fixture image.

Privacy coverage is distinct from vulnerability coverage: scan the full candidate
Git history (not a shallow/feature-range substitute), tracked source, actual build
context, exported source/package archive and every saved OCI layer, including a
file removed by a later layer. Do not follow archive symlinks or materialize unsafe
paths. Bound archive/member handling and fail on malformed/omitted layer inventory.
Use runtime-generated inert credential markers to prove archive/deleted-layer
detection. Keep raw sensitive diagnostics private; publish sanitized counts/IDs.

Producer controls: private0700 run directory, exclusive0600 evidence files,
explicit argv/no shell interpolation, at most32MiB per JSON/SBOM parser input and
180s per scanner subprocess initially. Record actual resources; deadline failures
require diagnosis, not automatic timeout inflation or silently truncated output.

## P2 — shared real browser inventory (05-11/12/16)

Freeze dev-only @playwright/test1.64.1 → playwright1.64.1 → playwright-core1.64.1,
Apache-2.0/Node>=20, no peers. Preserve existing runtime lock resolutions and
Vitest/TypeScript/Vite pins. Pinned Linux revisions: Chromium/headless1248
(156.0.8078.4), Firefox1555(157.0), WebKit2370(27.2), FFmpeg1013.

Supported reference host is Ubuntu26.04.1 x86-64/Node24.21.0; CI Ubuntu24.04 uses
its own official native dependency list (ICU/codec versions differ). Validate
installed native prerequisites before launch. Prefer isolated user-owned tooling
and libraries; any necessary host package installation needs concrete tool
authorization. Never silently substitute another engine or unsupported OS image.

Keep all existing CDP suites. Create a fresh real App/PostgreSQL fixture/schema
per engine, same critical spec, sequential engines/workers1/retries0. Secrets use
private fixture IPC/files, not CLI args/public artifacts. Required inventory:

| ID | Real scenario and required oracles per engine |
| --- | --- |
| B01 | First administrator/password/QR/manual TOTP, invitation enrollment, login/logout, replay/session boundaries |
| B02 | Project/task/subtask/milestone preview/apply/cancel, weekday participation, calendar and capacity outcomes |
| B03 | Actual delayed responses/A→B→A, stale completion, logout/relogin; real transport/roles/CSRF, no guard mocks |
| B04 | >1000 facts, subtree/employee/date filters/totals/revision/cursor, full JSON/CSV/HTML and portable acknowledgment |
| B05 | Native Gantt move/resize/ports/edit/cycle, map wheel/pan/node/bundles/collapse/Trace and preserved camera |
| B06 | Task/link/menu keyboard, drafts/busy/Escape, visible focus/return, member status payload and failed retry |
| B07 | Real print-popup contents and print invocation per engine, blocked popup/failure cleanup, clipboard supported/denied paths without reading the user's clipboard |
| B08 | Bug Report review/privacy and public-draft content; arbitrary external navigation blocked, no real Issue submission |
| B09 | Missing engine/prerequisite, zero/skipped/missing scenario and unsupported platform behaviour are explicit failures |

Functional journeys may retain the existing self-signed test-server exception
ONLY for the generated loopback fixture with strict origin/port/ownership checks;
never accept an arbitrary endpoint. Label this as functional coverage, not positive
browser CA trust. Separate strict browser contexts must reject unknown-CA/wrong-
hostname fixtures; existing trusted-CA proxy/curl checks isolate hostname/trust
validation. Record actual engine errors and do not infer isolated hostname cause
from a fixture also failing CA validation. No real endpoint loses verification.

Default traces/HAR/storageState/screenshots/videos are off. Failures expose only
static case/phase, engine/revision, status, exception class and source basename/
line. Deliberate password/token/TOTP/cookie/fragment markers must be absent from
public output; explicit debug artifacts remain private, never auto-uploaded.
No page.pdf claim for Firefox/WebKit: PDF is the user's browser-print flow.

Initial bounds: action10s, navigation/launch15s, assertion5s, enginejourney150s,
engineprocess180s, Go parent570s/runner600s. Missing/retried/skipped required cases
cannot count PASS. Freeze and validate the expected inventory/engine receipt.

## P3 — matching restore and genuine upgrades (05-05–10/13–17)

Keep format2/current14-member allowlist, <=1MiB per secret and<=512MiB archive.
First establish deterministic online-coherence RED using actual dump/restore:
real pg_dump returns, controlled synthetic writer commits synchronously, current
manifest counts run, actual restore disagrees. No sleeps or mocked dump results.
Only after actual RED/document review use one read-only REPEATABLE READ exported
snapshot for counts and pg_dump --snapshot. Keep its exporter alive until both
finish; close/rollback/process-clean on every cancellation/error. No blanket
write-stop requirement, archive expansion or new backup architecture.
Private helper `capture_database(container)` returns dump bytes and the existing
counts text. `BackupCoherenceProtocolTests` covers strict bounded protocol, owner
close on failure, timeout child termination and no archive publication on failure.
The manifest timestamp is assembled after capture; record snapshot start/end
bounds rather than claiming that timestamp is the exact database snapshot instant.

Scope restored secret verification to existing crypto/AAD and bounded restored
fields, with aggregate/category output only. Distinguish wrong backup AES key from
a correctly encrypted archive containing wrong application encryption.key and a
valid recomputed manifest. The latter must fail usable recovery even if readyz
passes. Do not redefine normal API readiness or expose public crypto diagnostics.

Freeze configuration/key/init-secrets/updater edits during capture. Operator
fixture preflight compares app runtime files with `app_secrets`,
`migration-config.json` with `migration_secrets/migration-config.json`, and
`database.password` with `db_secrets/password`. `instance.id` and legacy
`bootstrap.token` are host-only; do not require them in app_secrets. Resolve only
the fixture's explicit installation/container, never an inferred live workspace.
Keep the existing backup CLI/updater caller compatible. Compare before
quiescence and through the window; reject staged mismatch, never overwrite live data as repair.
Document host-root concurrent tampering as outside enforceable application locks;
sealed-data checks remain required even when file hashes match.

Reference fixture:5projects×300tasks/30staff, >=1001 facts, multi-year calendars,
hierarchy/dependencies and real synthetic credentials/sealed enrollments/outbox/
directory data. Old sources get only fields/schema they actually support. Use
actual production bootstrap/migrator/runtime roles and matching candidate image,
not a broad test role or copied binary in a PG container. The existing
`backup.verify/runtime_smoke` stays a compatible supplemental readiness check;
add scoped sealed-field validation so wrong application keys cannot return a
successful runtime_verified result. The primary R01/R04 packaged recovery command
and documented procedure must prove usable restored secrets and fail on their
absence; readiness alone is insufficient. Prove actual password+
TOTP login/replay, pending enrollment/invitation activation+one-use/expiry,
decryptability, role restrictions, read/write/export and restart persistence.
Do not reset TOTP last-used steps or extend token TTLs to manufacture usability.
Preserve public Origin/instance links, or explicitly prove/document required reissue
after changed origin; no silent rewrite of sealed payloads. No real outbound mail,
LDAP/calendar: isolated fake endpoints or delayed queue, with no-send oracle.

Run three fresh distinct origins ab4abfe/c8c34ae/e97971d into the final candidate,
plus controlled failed-upgrade/matched snapshot rollback for each. ab4/c8 require
operator assistance. e979 app-only eligibility must be recomputed from the final
six-file CONTRACT; scoped image/security changes may require operator assistance
there too. Preserve infrastructure_change_requires_operator. Same-candidate
rollback or old c8→WP01 evidence does not replace these fixtures.
Inject controlled operator activation failure only in the fixture wrapper after
the real candidate migration: create a synthetic marker/change, then fail before
candidate API/worker/proxy start. Recover the pre-upgrade matching dump/config/
operator artifacts and old image. Do not patch production images, add a production
fault flag or start the old image over the changed database.

Separately restore/test operator-managed report volume, TLS cert/key/proxy and
deployment/instance artifacts; exclude them from format2 and keep backup/support
private keys outside archives/distribution. Test wrong key, tamper/truncation,
missing config/secret, unsafe paths/symlinks/oversize, DB/disk/cert failures,
concurrent/interrupted migrations and actual least privilege. No partial readiness
or image-only downgrade is accepted.

RTO clock: prepared operator begins documented recovery → usable authenticated
read/write/export/privileges and restart, <=30min on the reference fixture.
Prepared images/source/keys are explicit prerequisites, measured separately.
RPO is the last verified matching snapshot/acknowledged write; record later lost
writes and snapshot age. No daily cadence or universal loss/downtime promise.

## P4 — exact-source final soak and acceptance

Preserve the WP-02 reference profile and seed:5×300tasks,30sharedstaff,10distinct
authenticated users,90/5/5 read/preview/save mix,1logicalaction/user/second and
stagger. Keep10min mixed scheduling plus a full60min soak scheduling (drain and
preparation outside); do not count the old25min run or smoke as final evidence.
Readp95<=500ms, preview/savep95<=2s and zero unexpected errors remain the accepted
reference criteria. Preserve real revision/data/audit oracles, independent drain,
no automatic mutation retries, no excluded late cancellation/timeout/lost ack.
Record exact source/harness/runtime metadata, machine/cgroup resources, DB/CPU/heap
series, rates/counts/latencies and quiet execution window. No competing DB/Chrome
benchmarks or code changes during the hour; READ-ONLY review may proceed.

Required candidate CI must run three browsers, all-image gates, existing frontend/
unit/API/security/backup/upgrade/HTTPS/install/updater checks with expected roots
and no hidden skip. Long reference-load evidence is separately mandatory for the
same final runtime/harness SHA; scheduled success on another SHA cannot substitute.
Freeze exact runner entry points and guards before adding them. Preserve legacy
runner semantics and explicit disposable-DB validation; a focused command must
never masquerade as full acceptance.

Finish with final clean source, locks/OpenAPI consistency, complete scanner/privacy
receipts, combined regression, all accepted operational cases, exact-SHA CI and a
different READ-ONLY reviewer. Any later runtime/harness fix requires relevant
rerun and review; do not accept by budget exhaustion or only partial greens.
Record unresolved findings separately. Only then label WP-05 PASS — INDEPENDENTLY
VERIFIED; deployment/main/release still require their separate authorization.
