# Evidence, independent review and acceptance

## Status model

`PROPOSED → APPROVED FOR IMPLEMENTATION → IMPLEMENTED / VERIFICATION PENDING →
VERIFIED BY IMPLEMENTER → INDEPENDENT REVIEW → PASS — INDEPENDENTLY VERIFIED`.

Use `BLOCKED` for missing required decisions/evidence and `FAIL` for an observed
failed criterion. A documentation review may conclude `PLAN REVIEWED`; that is
not implementation acceptance. Current WP states are in the programme README. Authoring this
plan, a successful baseline CI, or an agent's summary cannot close a WP.

## Required WP evidence record

Create one record per WP implementation, with these fields:

- WP ID, approved scope/decision IDs, final source SHA and clean-tree status.
- Exact changed files and justification mapped to approved scope.
- Fixture definitions/seeds, frozen clock/timezone, hardware/runtime/image/browser versions.
- Reproduction: old behaviour, expected behaviour, failing baseline test evidence.
- Test matrix: ID, exact command, exit code, PASS/FAIL/SKIP, UTC time, artifact path/hash.
- Deterministic assertions, negative cases and independent expected-value derivation.
- Compatibility/migration manifest before/after: schema version, IDs/counts/digests,
  retained historical fields, export/backup roundtrip, repeated migration outcome.
- Performance evidence: workload settings, raw samples, percentiles, error counts,
  query/CPU/RAM data; identify cold/warm runs and environmental limitations.
- Security evidence: exact image digest, scanner/database timestamps, all findings,
  disposition, human exception records if approved, no masked or expired exceptions.
- Limitations, remaining risks, unmet acceptance conditions and rollback procedure.
- Independent review identity, reviewed SHA, findings, retest results and acceptance.

Keep private logs, database dumps, credentials and real customer records out of
Git and Issues. Publish sanitized fixture evidence, checksums and reproducible
commands. Hashes identify artifacts; they do not prove that a test ran. Preserve
actual logs privately where needed, and verify CI artifacts before trusting them.
Do not rely solely on screenshots, coverage percentages, unit tests mirroring the
implementation, or an aggregate "all passed" line.

## Independent READ-ONLY review checklist

Reviewer must be a separate agent/person, not the implementing writer. Review may
read code/docs and run isolated tests; it must not patch code or production state.

1. Read accepted requirements, authoritative decisions and current code; resolve
   conflicts rather than assuming either documentation or reports are correct.
2. Check diff scope and final SHA; verify every acceptance ID has actual evidence.
3. Inspect assertions and fixtures for circular oracles, missed negative cases,
   skips, flakes, hidden retries, generated-only expectations and leaked real data.
4. Independently reproduce representative critical paths and failure modes:
   WP01 year boundary/calendar gap; WP02 delayed stale responses/concurrent saves;
   WP04 >1000 traversal/DST; WP03 extracted state behaviour; WP05 restore/access.
5. Inspect authorization, transaction boundaries, migration compatibility and
   resource bounds, including interactions with other completed WPs.
6. Compare results to frozen budgets; unresolved proposed thresholds mean the
   corresponding performance/operational criterion is not accepted.
7. Report findings with severity, file/test references, reproduction and expected
   correction. Distinguish observed defects from hypotheses or optional improvements.
8. Re-review changes after fixes. Acceptance attaches only to the final reviewed
   implementation SHA; subsequent material changes invalidate affected acceptance.

A blocker is data loss/corruption, unauthorized access, a broken core acceptance
criterion, an untested destructive migration/restore path or an unresolved required
architecture decision. Only the human owner can approve an explicit exception;
record its scope, expiry, residual risk and compensating controls. Never change a
failed result to PASS because the deadline is near.

## Final combined acceptance

Run from the combined release candidate, not a mixture of individually tested
branches. Include installation from clean source archive, upgrade from approved
old schema fixtures, encrypted backup and restore on an empty isolated installation,
rollback rehearsal, full scheduling/history regression, concurrent clients,
accepted load/soak profile, all browser engines and all required image/security
checks. Restore must demonstrate usable accounts/TOTP and decryptable settings,
not just successful pg_restore or matching row counts. No live email/calendar/LDAP
calls during fixtures; use synthetic local endpoints with verified TLS.

Freeze exact release SHA/image digests and an acceptance manifest. Any unresolved
blocker prevents final operational acceptance. A passing documentation-only CI
is not final acceptance. Production rollout and data transfer remain separately
recorded operations after the required authorization.

## Proportionality

This protocol is sized for the significant WPs in this programme. Documentation
wording/link corrections require diff/link review, not a full load test. Small,
low-risk implementation fixes use the smallest meaningful regression suite.
Architectural changes, dependencies, migrations and security exceptions still
require the explicit approvals listed in the development workflow.
