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

Finish and review source/build/package/all-layer privacy, all-image actual scans,
D14 protected approval binding (no individual exception currently approved),
coherent backup C01–C03, real R01–R08 and three fresh U01–U06 origins with rollback,
all three actual browser journeys, required CI and exact-source 600+3600-second
load. Then obtain independent final code/contract/evidence review. A partial green
or scaffold commit cannot confer WP-05 PASS.
