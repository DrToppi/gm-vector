# Multi-year planning and managed updates

Status: requested 2026-10-30. Update entry point B and calendar option B were accepted. Scheduling budget follows D02 option B.
The user confirmed projects must be able to span more than one year.

## Original baseline evidence

At the original baseline, `readCalendar` loaded only the project's single calendar_id. Imported calendars
were complete immutable annual versions. The bundled dataset contained only 2026.
`Calculate` rejected a date missing from that calendar. Task DurationDays and
capacity search also stopped at 730 days. These were separate restrictions;
simply raising a number could not fix missing calendar coverage. See the current
[WP-01 contract](remediation/2026-10-30/wp-01-implementation.md) and
[verification record](remediation/2026-10-30/wp-01-verification.md) for the repair.

## Calendar options

| Option | Benefits | Costs and risks |
| --- | --- | --- |
| A: explicitly selected immutable calendar version for each year | Accurate and reproducible; no silent choice between competing versions. | Operator must import/select each required year; calculation stops with a specific missing-year message. |
| B: annual verified calendars plus provisional Monday-Friday fallback for missing years | Long-range planning works before official calendars are available. | Future holidays/transfers are unknown; visibly provisional dates must be recalculated through preview when verified years are supplied. |
| C: project-owned multi-year manual calendar | Flexible across regions and custom schedules. | Manual maintenance, more complex versioning/UI and risk of incorrect holidays. |

Recommendation: B for long-range planning, with explicit warning and confirmation
before replacing provisional schedules. A is preferable when only verified dates
are acceptable. Independent scheduling safeguards must bound work without silently
truncating results; supported horizon/limits must be documented and tested.

## Update options

| Option | Benefits | Costs and risks |
| --- | --- | --- |
| A: host-side update command (recommended) | Fetches official Git source, resolves immutable commit, builds separately, backs up and installs; no Docker access in web application. | Host operator runs the command; compilation needs toolchain, network, time and disk. |
| B: administrator button backed by a separate host updater | Convenient progress and version selection in UI. | Adds a privileged service and authenticated IPC; needs strict request authorization and a restricted update source. |
| C: scheduled host updater | Hands-off installation during a maintenance window. | Unexpected downtime or migration failures without an operator; requires scheduling and alerting. |

Required contract for every option: an allowlisted official HTTPS repository, no
embedded Git credentials, isolated checkout/build, explicit target commit recorded,
update lock, preflight/disk/health checks, encrypted backup plus matching config,
retention of prior image IDs and a durable operation record. Do not overwrite an
operator's source edits. Download/build failure must leave the live deployment
untouched. Stop writers before the final backup/migration window. A failed
migration cannot be rolled back merely by starting the old image; restoration of
matching database/config must be explicit and tested. Never delete user volumes.

See [accepted updater contract](updater.md).

Default release policy: operator-selected release tag or commit;
tracking main only as an explicit choice. No automatic internet check or update
until configured. Image/source authentication follows the selected official
repository and optional signed release provenance; a SHA alone is not a signature.

Primary documentation checked 2026-10-30:
[Git fetch](https://git-scm.com/docs/git-fetch),
[Docker Compose up and volume preservation](https://docs.docker.com/reference/cli/docker/compose/up/).
