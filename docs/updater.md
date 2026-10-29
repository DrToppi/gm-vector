# Managed source updates

Status: implementation requested by the user on 2026-10-30. Entry-point choice
B (administrator UI plus separate host service) was explicitly accepted; see [alternatives](multiyear-and-updates.md). This document
bounds the shared safety contract before code is changed. The repository is no
longer restricted to documentation-only work for the requested updater; other
work-package architecture decisions remain unresolved.

## Scope

Download source from the official public HTTPS Git repository, resolve the target
to an immutable commit, build in a separate directory, create a matching encrypted
backup and install pinned images into an existing installation. No in-place git
pull over the operator's working tree. The application API never receives Docker
or arbitrary-shell privileges. Host administrator remains the trust boundary.

The user selects a commit or release ref; following main is explicit. No periodic
background internet access is enabled implicitly. Only official repository URLs
are accepted; no embedded credentials, alternate transports, hooks or submodules.
A resolved SHA identifies fetched source but is not a publisher signature.

## Required operation stages

1. Validate private state, tooling, Docker context, running image identities and
   installation identity; acquire a per-installation lock. Refuse an interrupted
   unresolved operation or a nonrunning/unidentified installation.
2. Fetch into a new private build directory, resolve and record commit, build
   candidate app/proxy images separately. Preserve PostgreSQL image/major version
   in this updater; major database-engine upgrades require another procedure.
3. Preflight candidate TLS/config and restore-test tooling. Verify candidate
   migrations on a disposable clone before touching the working database.
4. Record previous installer source/image IDs/config digest and current candidate.
   Stop API/worker/proxy writers, then take the final encrypted DB/config backup.
   Existing volumes and installation secrets are retained.
5. Record migration-start durably before invoking migration/installation. Apply
   candidate with pinned image IDs, preserving existing origin, TLS and identity.
6. Verify exact image IDs, HTTPS readiness and installed software commit. Commit
   installation metadata only after verification; retain previous artifacts.

Failure before migration permits restarting previous services. Once migration
may have run, never automatically run an old binary against the changed database.
Keep writers stopped and provide an explicit recovery action from the recorded
backup and previous images. Recovery must validate installation identity and
restore matching DB/config, not delete a volume or regenerate encryption keys.
Any destructive recovery needs an explicit, concrete operator command.

## Planned acceptance

| ID | Scenario | Required result |
| --- | --- | --- |
| U01 | Normal update from approved old commit to fetched target | Exact fetched commit and pinned images installed; fixture data, auth/TOTP and configuration retained |
| U02 | Invalid/option-like ref, alternate repository, credentialed URL | Rejected before fetch or execution |
| U03 | Failed fetch, build, missing tools or low disk | Working deployment untouched; no stop/migration |
| U04 | Two concurrent update commands, symlink state/lock | One owner only; unsafe paths rejected |
| U05 | Backup/verification failure after writers stop | No migration; old services recover; original data digest retained |
| U06 | Migration failure, post-migration health failure or interruption | Durable needs-recovery state; no old-binary automatic restart |
| U07 | Explicit recovery of the same failed operation | Matching backup/config/old images restored and verified, no cross-installation restore |
| U08 | Interrupted operation restart and repeat apply | Refuse unsafe continuation; explicit status/recovery; no double migration or overwritten journal |
| U09 | Foreign backup/identity, corrupted envelope or metadata | Reject before changing DB/state |
| U10 | Success/failure logs, exported files and source package | No passwords, tokens, private keys or customer data in logs/repository |
| U11 | Custom PEM/port, rootless and rootful Docker | Config and origin retained, trust/hostname checks enforced |
| U12 | Source checkout has local edits | No modification to that checkout; independent release directory |

Results remain pending until tests execute. Significant implementation receives a
separate read-only review against its exact final SHA before acceptance.

Sources checked 2026-10-30: [Git fetch](https://git-scm.com/docs/git-fetch),
[Compose lifecycle](https://docs.docker.com/reference/cli/docker/compose/up/),
[PostgreSQL restore](https://www.postgresql.org/docs/current/app-pgrestore.html).

## Accepted entry point and implementation boundary

Option B accepted by the user on2026-10-30. The host service runs as the installation
owner with Docker access. The API receives a Unix socket and a separate bearer
credential; neither Docker socket nor generic commands are exposed to the API.
Only browser-session administrators can check/apply/recover. Existing CSRF and
origin checks apply. Each apply requires typing the full candidate commit;
recovery requires typing the recorded operation ID. The service validates both
independently. No arbitrary repository URL or filesystem path is accepted over IPC.

The first updater preserves the PostgreSQL and proxy images and installation
configuration; it updates application code only. It refuses a candidate whose
DB/proxy/Compose deployment contract differs from the installed source. A broader
infrastructure update needs an explicit operator procedure. This bounds rollback
and avoids silently changing TLS/network/database major versions. New application
SQL migrations are permitted and are rehearsed on a disposable restore first.

The service is installed explicitly through a host command and user systemd unit.
It never installs a host service from an anonymous web request. Source builds run
with the host operator's account; the official repository is therefore a code
execution trust boundary. The service is optional/disabled until configured.

## Setup (source installation)

Build and deploy the accepted updater commit with the normal installer first;
`prepare` records actual running image IDs and the application's embedded commit
in private `state/deployment.json`. Do not hand-edit that identity to bypass checks.
Install Python cryptography, Git, Go, Node/npm, Docker/Compose and OpenSSL on the
host. Allow at least 2 GiB free build space plus space for the actual database,
backups and retained failed databases; the 2 GiB check is only a minimum floor.

From a clean checkout of the exact deployed updater commit:

```sh
python3 production/deploy/configure-updater.py --state /absolute/private/state
python3 production/deploy/install.py prepare --state /absolute/private/state
systemctl --user daemon-reload
# Use the exact generated unit name printed by configure-updater.py:
systemctl --user enable --now UNIT_NAME
```

The unit uses the installation owner's Docker context and runs unprivileged as
that host user. It is not a root daemon. For startup without an interactive login,
the operator must configure user-service persistence through their normal host
administration procedure. Retain the configured source checkout unchanged at its
path; updating the host agent itself is an explicit operator action in this first
version. The web feature updates application images, not this privileged agent.
Offline/archive installations need a clean official source checkout to configure
this optional source-building host service.

In Management → Update from Git, explicitly check a branch/tag/commit, review the
resolved full SHA, then type that SHA to install. Candidate must descend from the
installed commit; downgrades and unrelated histories are rejected. The status
endpoint does not itself fetch or install anything. No update starts merely by
opening Management.

The IPC directory is mounted only into API, not worker. Socket permissions allow
the container identity to connect; the enclosing host state stays private and
Bearer authentication is independently checked. The credential is stored as a
0600 host file, copied into app secrets and included only in encrypted backups.
IPC accepts at most eight simultaneous handlers, 16 KiB command bodies and fixed
operations. Rehearsal PostgreSQL has a 1 GiB memory, two-CPU, 256-process and 768 MiB
tmpfs ceiling; a larger database fails safely and requires a later approved limit
rather than silently skipping rehearsal.

## Maintenance, failures and local recovery

The API and public proxy become unavailable briefly during migration. The browser
reconciles status after reconnecting and never retries an apply automatically.
Candidate API image/commit/internal readiness are checked before publishing proxy;
worker starts last. On failure and interrupted-service restart, stop and confirm
writers before offering recovery. If Docker cannot confirm the stop, preserve a
`stop_unconfirmed` journal flag and require local intervention.

A failed migration may leave the application API unavailable, so browser recovery
cannot be guaranteed. The local host service remains accessible through its Unix
socket. Run from the unchanged configured source checkout:

```sh
python3 production/deploy/update_service.py status --state /absolute/private/state
python3 production/deploy/update_service.py recover --state /absolute/private/state \
  --operation-id EXACT_RECORDED_OPERATION_ID
```

Recovery explicitly returns to the stopped-writer backup. Any changes accepted
between activation and a later failure will not appear in the restored snapshot;
the failed database is retained under an operation-specific name for inspection.
Do not recover a successfully updated installation merely to reverse edits.
Recovery validates the same installation identity and archive hashes/decryption,
restores to a new database, checks counts, then atomically renames databases.
It retains the failed database and all volumes. Retry recognizes a committed
name switch and does not overwrite a recovered database with the dump a second
time. Failed partial restore databases remain for explicit operator cleanup.

Operation records and retained source/build/backup artifacts are private host
state. Automatic retention/cleanup of these artifacts is not enabled. Monitor
space and remove old completed operations only after the recovery window. Never
remove the active operation or the configured source/service files.
