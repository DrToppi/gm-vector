# WP-05 implementation evidence

Status: **IN PROGRESS — NOT OPERATIONALLY ACCEPTED**. Updated 2026-11-02.
Follow the [admitted bounded plan](wp-05-implementation.md). Working installation,
main and releases have not been changed by this package.

## Scanner source slice

Source `6e8db9f24ab44e9b1dbd0196759375e5d0b87b08` received a separate READ-ONLY
review and 23 independently executed passing Python units. Controls cover image/
archive/report/SBOM membership, advisory age and epoch, malformed input, bounded
transport and owned-container cleanup, UNKNOWN/unfixed blocking and disabled
exception admission. An actual read-only old-image probe rejected a mismatched
source SHA with exit 2 before archive creation; its evidence directory/files were
0700/0600. This is not a full vulnerability scan, provenance/privacy acceptance
or an approved advisory exception. Later helper changes need relevant review.

## C01 online backup coherence — actual RED before correction

On 2026-11-02 the unchanged C01 diagnostic ran against the old capture logic with
real isolated PostgreSQL, actual pg_dump, a synchronous committed writer after
dump completion, and actual pg_restore. It exited **1** at the intended oracle:
the restored dump retained the pre-write rows, while the manifest counted the
post-write rows. No mocked dump result or sleep-based race was used.

Capture source was HEAD `6e636fa039f5ea0f20f66357faef96f0906ef456`;
the new, then-uncommitted C01 script SHA-256 was
`7cb1634ec5b3a6b0c3524083da95cd626b652d895f4b4b2e7ee54a41f9bea4ba`.
Private RED log SHA-256:
`8b922d1fe1916d7ab78d2a07374c7af74182e2341448ac1b398f2d104f0dc3d8`.
Other writers' unfinished files were present; this was a focused regression,
not clean-source package acceptance.

Command: `python3 production/scripts/backup-coherence-integration-check.py
--evidence-file <new-private-file>`.

The earlier sandbox attempt failed at Docker access before fixture creation and
is **not** this RED. The pending retry was interrupted by the orchestrator for
status, not rejected by the user. After explicit Docker tool authorization, the
genuine run above reached `C01 manifest counted a different snapshot than the
actual restored dump`. The snapshot correction has not yet been accepted.

Crypto/protocol checkpoint `fd37fbd` and genuine recovery-fixture checkpoint
`3955caa` have passing focused units/syntax checks, but no real packaged recovery
or origin-upgrade result is claimed here.

## Snapshot correction and real cancellation follow-up

Checkpoint `72243cc` shares one exported PostgreSQL snapshot between counts and
dump. C01–C03 actually passed in the isolated Docker fixture (exit 0); private log
SHA-256 `10d088b266c70602b98356ffb0407017360c1c42a75caf96274f59a3e9b9416f`.
The initial C03 used an honestly labelled injected timeout after a real exporter
started. It did not establish remote pg_dump cleanup after killing Docker CLI.

A stronger deterministic follow-up on 2026-11-02 produced an actual **RED** at
unchanged production `72243cc`: real exporter AccessShare lock, queued exclusive
locker, real pg_dump observed blocked, then a test-only 50 ms communication
deadline. The Docker CLI was killed but the remote pg_dump session remained,
failing the zero-owned-session oracle. Fixture cleanup terminated only its owned
dump/locker and removed the disposable container.

Frozen failing harness SHA-256:
`3086b2c8872d39b31fab9a30db4c96642f1b10f4c3788c6053462f42ef4b8d11`;
private RED log SHA-256:
`68f5a9ced1d6216f017ed1805bd564e0ce92321cbee082e948eb0ee7093bbcb4`.
Allowed narrow correction: random per-dump PGAPPNAME, structured exec arguments,
and a bounded cleanup query terminating only that exact owned dump session on
capture failure. Preserve archive/CLI/updater contracts; do not terminate other
database sessions. This correction still needs actual GREEN and review.

The genuine R01–R08 tool diagnostic also passed against clean accepted WP-04
`e97971d` and its matching pre-existing images. It covered 5×300 tasks, 30 staff,
1001 facts, production DB roles, restored credentials/invitation/replay, write/
export/restart, operator artifacts and wrong application-key rejection despite
readyz. Measured prepared recovery action was 19.798 seconds. Log SHA-256:
`2e3b47e0af8be27a5b4acdce860beaa6ac3ba1108cc10c1be4d1b61db9c2e446`.
This validates the tool on that older accepted runtime; it does not replace
final-candidate recovery or three origin-upgrade acceptance.

## Browser scaffold

Checkpoint `6e636fa039f5ea0f20f66357faef96f0906ef456` contains the three-engine
fixture and B01–B09 scenarios. Browser TypeScript, lint/format and no-DB admission/
privacy controls were executed. Real journeys remain unexecuted. READ-ONLY review
identified bounded oracle corrections for certificate-error classification,
actual clipboard capability, resized dates/duration, a real Trace chain and
retained actual browser/revision evidence; these must be resolved before acceptance.

Pinned browser installation received tool authorization, but the CDN download
ended with ECONNRESET before completion. This is not a missing-engine skip or
passing browser evidence. Native prerequisites and successful three-engine
launches still require verification.

## Remaining acceptance

D14 environment security-exceptions was created on 2026-11-03 with numeric ID
26111380009, sole reviewer161593815 and self-review prevention false. Contrary to
the earlier published-schema assumption, actual REST2022-11-28 GET exposed
can_admins_bypass. PUT false succeeded and a separate independent read confirmed
literal false. Require that exact typed state on every admission and fail closed
on absence/change. No UI action, approval event or individual waiver was used.
The helper/workflow admission path remains unverified and disabled.
Actual settings SHA-256:
`daaf21593602774b044146cd667270adf0d53e0355625ec8145e4f8b7c45c886`;
saved API readback evidence SHA-256:
`0b2b9bd8d3009ebe008764d7f90626ff2de241f486f3978a833aa15a26f15ea8`.
The nonsecret setup record is .github/security-environment.json; it cannot
authorize a waiver or replace the per-artifact owner approval.

Finish and review source/build/package/all-layer privacy, all-image actual scans,
D14 protected approval binding (no individual exception currently approved),
coherent backup C01–C03, real R01–R08 and three fresh U01–U06 origins with rollback,
all three actual browser journeys, required CI and exact-source 600+3600-second
load. Then obtain independent final code/contract/evidence review. A partial green
or scaffold commit cannot confer WP-05 PASS.
