# Implementation checkpoint — 2026-10-30

Status: **RESUMED BY EXPLICIT USER REQUEST**. The pause below is historical.
The reviewed updater commit has now been deployed and its host service enabled.
Current work: WP-02 under accepted decisions D03 A/D04 A, after WP-01 acceptance.

## Completed and verified

The Git updater is implemented in `feature/git-updater`. The independently
reviewed implementation commit is
`c8c34ae211e185d2acbced0aead9549c4015c7d7`.

- Administrator UI and authenticated Unix IPC to a separate host service.
- Official advertised Git branch/tag selection, pinned commit and isolated build.
- Stopped-writer backup, migration rehearsal and application image activation.
- Explicit snapshot recovery, retained failed database and restart/retry handling.
- Local independent status: **PASS — INDEPENDENTLY VERIFIED** for the updater
  implementation at the commit above.
- [CI run 40968484550](https://github.com/DrToppi/gm-vector/actions/runs/40968484550)
  completed successfully for that exact commit.

See [updater verification](updater-verification.md) for executed evidence and
[operator contract](updater.md) for setup, recovery and limitations. This acceptance
does not close any remediation WP or establish final operational acceptance for
the whole application.

## Deployment boundary at pause

The existing running local application has **not** been switched to the updater
implementation. It remains on `ab4abfe` (keyless first administrator setup).
A separate detached source checkout for `c8c34ae` was prepared and its application
image build completed. A local image tag alone is not proof that a container was
recreated; use actual running image IDs and embedded release metadata when resuming.

The following operations have **not** been performed on the working installation:

- Installing the updater commit with `install.py prepare`.
- Configuring its API socket/token through `configure-updater.py`.
- Enabling or starting the generated host user-service unit.

No merge into `main` has been authorized or performed for these changes. Private
state, source checkout paths, credentials, keys, backups and raw logs stay outside
this public checkpoint.

## Accepted next decisions at pause (historical)

The user selected WP-01 decisions **D01 B** and **D02 B**:

- Missing annual calendars use provisional Monday–Friday working days with a
  visible warning. Replacing provisional dates with verified annual versions
  requires preview and explicit confirmation.
- Scheduling follows available calendar coverage with an explicit computation
  budget and a clear failure on exhaustion, replacing the fixed 730-day search.

At the pause, WP-01 implementation had **not started**. Numeric budgets and boundary evidence
still need to be frozen in its bounded implementation plan. Remaining decisions
for WP-02/04/03/05 retain their recorded proposed status.

## Resume sequence

1. Confirm branch, clean-tree status, approved implementation SHA and actual running
   deployment. Preserve this checkpoint and any later user changes.
2. Take a matching encrypted backup before deployment. Install the reviewed updater
   source/image into the existing instance without deleting or reseeding its data.
3. Configure the optional service from a stable detached source checkout, refresh
   staged configuration, enable the generated unit and verify actual admin IPC.
4. Record successful deployment separately; do not mark it done from a built tag.
5. Start WP-01 document-first using D01 B/D02 B, then continue in the approved order
   **WP-01 → WP-02 → WP-04 → WP-03 → WP-05**, resolving pending choices before coding.

These steps describe the saved continuation point. They are not permission to
resume work while the user's pause remains in effect.


## Resume result

After the explicit resume request, the working local application was switched to
reviewed commit c8c34ae. A stopped-writer encrypted backup was recorded before
installation. Actual deployment metadata and verified HTTPS confirm the installed
commit. The optional host service is enabled and active; authenticated Unix IPC
returns enabled=true, state=idle and the installed commit. The service uses a
separate detached source checkout, independent of the development working tree.
No merge into main occurred. WP-01 implementation follows next.

## WP-01 acceptance and next package

WP-01 is **PASS — INDEPENDENTLY VERIFIED** at
`5812b2c72855bcc0297553b2520f684c088c1f45` with
[successful exact-SHA CI](https://github.com/DrToppi/gm-vector/actions/runs/41021707647).
See [the full evidence record](remediation/2026-10-30/wp-01-verification.md).
The working installation remains on reviewed updater c8c34ae; WP-01 has not been
deployed there and main has not been merged.

The user selected D03 A (ordered/versioned polling, one active refresh and hidden
pause) and D04 A (profile then optimize the existing on-demand calculation).
Continue with the [WP-02 bounded contract](remediation/2026-10-30/wp-02-implementation.md).
After the first baseline, the user accepted the five-project/ten-user reference
criteria: read p95 ≤500 ms, preview/save p95 ≤2 seconds, no unexpected errors,
ten minutes of mixed load and fifteen minutes of soak. The sixty-minute soak is
reserved for final WP-05. Do not infer an acceptance result from short benchmarks.
Keep the prior unexplained E2E failure and generic server diagnostic visible for
the broader WP-05 stability/diagnostic work.
