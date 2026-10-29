# Updater verification record

Implementation branch: feature/git-updater. Initial implementation730854b;
nonroot PostgreSQL rehearsal fixes5c42cd1/03acb4a. Final SHA and independent
acceptance remain pending until the final checks below complete.

## Executed evidence

- 31 engine/Unix IPC tests and 2 configuration tests passed. Includes auth/body
  bounds, duplicate headers/JSON, non-ASCII bearer, official advertised branches
  and peeled tags, changing refs, unadvertised fork SHA rejection, locks, restart
  and failure handling, stopped writers, restore counts and committed-switch retry.
- Go relay unit/race, administrator-session-only checks and pre-dispatch audit
  integration passed. Authorization matrix now covers55routes/385role cases.
- Seven locale catalogs and TypeScript/frontend build passed. Real React browser
  update test passed, including accepted-command/lost-ack reconciliation, stale
  response rejection and no destructive retry.
- Full verifier passed unit/API/security/backup checks but initially stopped in
  the new browser fixture. The fixture held a response before sending no-store
  headers, provoking Chrome same-URL cache locking. Sending headers then holding
  only the body fixes the harness; no production UI change or timeout widening.
- Actual official HTTPS Git fetch→candidate build→disposable Docker application
  update→failed-migration recovery passed on03acb4a. Project data, TOTP ciphertext
  and encryption key survived; migration-added table was removed by restoring
  the snapshot; repeat recovery did not restore/switch twice.
- A separate read-only reviewer reproduced official advertised checkout and the
  narrow unit suites; no live installation was used for test data.

## Independent findings resolved

Post-activation failures and service interruption now stop/confirm writers; failed
stop is journaled. Fresh installer records actual image IDs and release metadata.
Socket directory permissions are explicit under restrictive umask. Rehearsal
handles nonroot PostgreSQL images and supplies a valid migration runtime role.

## Limits

Recovery is explicitly snapshot-based, not zero-data-loss rollback; failed DB is
retained. Host agent self-update and infrastructure/major PostgreSQL upgrades are
operator actions. The application updater rejects changed deployment contracts.
No claim is made that all remediation WPs are complete. Final full verifier,
final Git/Docker run, CI and exact-SHA independent review must be recorded before
marking updater acceptance complete. Raw logs remain private local evidence.
