# gmVector

**Plan together. See the connections.**

gmVector is a self-hosted project planner with an interactive Gantt chart,
a dependency map, team capacity planning, and planned versus actual effort.
It is free and open-source software under the Apache License 2.0.

## What it does

- Organize projects into tasks and expandable summary tasks.
- Connect dependencies, inspect dependency chains, and adjust schedules.
- Plan work against project calendars, individual availability, and team capacity.
- Track time, discuss tasks, and export project data.
- Invite colleagues by email and protect accounts with TOTP.
- Integrate your own systems through a documented HTTP API.
- Connect a corporate directory for LDAP authentication and synchronization.
- Use nginx HTTPS with local or operator-managed certificates.

Authentication, project permissions, and request validation still protect your data.
External integrations are optional and must be configured by the operator.

## Get started

See [Installation](docs/installation.md) for prerequisites, first-run setup,
HTTPS, backups, and updates. The first administrator sets a password and
confirms TOTP before the workspace becomes available.

- [API and integration guide](docs/api.md)
- [Corporate directory](docs/directory.md)
- [HTTPS configuration](docs/https.md)
- [Administrator recovery](docs/recovery.md)
- [Localization](docs/localization.md)
- [Security policy](SECURITY.md)

Use synthetic data for evaluation. Back up both the database and the installation
configuration before an update; encryption keys are part of recovery material.

## Repository access

Everyone can read, clone, and fork this repository. Direct writes to the official
repository are limited to maintainers. You may propose a change through a pull
request; submitting a pull request does not grant write access.

See [Contributing](CONTRIBUTING.md). Please do not include credentials, private
project data, or unredacted diagnostics in issues or pull requests.

## Languages

The interface supports English, Russian, German, French, Spanish,
Brazilian Portuguese, and Simplified Chinese. The localization guide records
coverage and known limitations; translated user content is not sent to a service.

## License

gmVector's own code is licensed under [Apache-2.0](LICENSE). Third-party components
retain their own licenses and notices. Official release signatures establish
provenance; they do not restrict your ability to build or modify the source.
