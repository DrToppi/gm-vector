# Updater verification record

Implementation branch: `feature/git-updater`.
Reviewed implementation SHA: `c8c34ae211e185d2acbced0aead9549c4015c7d7`.
Status: **PASS — INDEPENDENTLY VERIFIED** for the locally reviewed updater
implementation. Its [CI run 40968484550](https://github.com/DrToppi/gm-vector/actions/runs/40968484550)
completed successfully for the same commit.

Deployment was completed after the explicit resume request: the working instance
reports c8c34ae, verified HTTPS is ready and the enabled host service returns
enabled=true/state=idle over authenticated Unix IPC. A stopped-writer encrypted
backup preceded installation. See [checkpoint](implementation-checkpoint.md).

## Executed evidence

| Check | Actual result |
| --- | --- |
| Host engine and Unix IPC | 31 tests passed: authorization/body bounds, duplicates, non-ASCII bearer, official refs/peeled tags, ref changes, fork SHA rejection, locks, interrupted state, stopped writers and recovery retry |
| Service configuration | 2 tests passed, including restrictive umask and private token/unit permissions |
| Go relay and audit | Unit/race and API integration passed; session administrators only, audit before dispatch, no secrets in relayed messages |
| Access control | 55 routes and 385 endpoint/role cases passed in the security matrix |
| Frontend | TypeScript, seven locale catalogs, build and real React updater-browser checks passed |
| Complete regression | CI passed the full verifier, including unit/race, API, backup, updater browser and existing real API/PostgreSQL browser E2E |
| Official Git forward update | Disposable installation started from `03acb4a`, fetched the official `feature/git-updater` tip, built and installed `c8c34ae`; assertions required different commits and application image IDs |
| Failed-migration recovery | Synthetic schema/data change followed by failure: writers stopped, matching snapshot restored, failed table absent, original project/TOTP ciphertext/encryption key retained |
| Recovery retry | Repeated recovery recognized the committed database-name switch and did not replay restore or switch the database twice |
| Container/installation CI | Proxy vulnerability gate, HTTPS, empty installation and updater maintenance/recovery fixture passed |

The forward test used `updater-integration-check.py --baseline-source` with an
isolated checkout of the older commit and `--fetch-ref feature/git-updater`.
It executed a real official HTTPS fetch and source build, not mocked downloads.
The default CI fixture separately exercises host IPC, maintenance and recovery;
the distinct-version forward upgrade was also run locally. All projects/accounts
in these fixtures were synthetic. Raw logs remain private local evidence.

## Independent review

A different read-only reviewer inspected the exact final implementation SHA,
requirements, test code and actual logs, independently reran the 31 engine/IPC
and 2 setup tests, and reproduced an official advertised HTTPS checkout. The
reviewer initially refused full acceptance of the same-SHA activation fixture;
the distinct-version/image regression above closed that evidence gap.
No blocking code findings remained at acceptance. This status does not apply to
other remediation WPs or to final operational acceptance of the whole application.

## Findings resolved during verification

- Post-activation failure and interrupted service startup stop and confirm writers;
  a failed stop is recorded rather than claiming maintenance is safe.
- A fresh installer records actual running image IDs and embedded release metadata.
- Socket directory permissions remain usable under restrictive umask.
- Rehearsal supports nonroot PostgreSQL images and a valid migration runtime role.
- PostgreSQL readiness uses TCP, avoiding the temporary initialization Unix server.
- The browser stale-response fixture sends no-store headers before withholding the
  body, avoiding Chrome cache locking; timeouts were not widened.
- IPC negative tests assert the actual HTTP rejection even when the server rejects
  headers before the client finishes sending a body.

## Limits

Only application code is updated automatically; changed infrastructure contracts
are rejected. Host agent self-update and major PostgreSQL upgrades are operator
actions. Recovery restores the stopped-writer snapshot and can discard later
changes; the failed database is retained. A local recovery command is required
when the API is stopped. Official source code is executed with host-operator
privileges during builds. See [updater contract](updater.md).
