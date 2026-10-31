# gmVector documentation

Start with the operator guides for the current application. Architecture proposals
and remediation plans are labelled separately; they are not promises that the
corresponding functionality is implemented.

Current work is [resumed from the documented checkpoint](implementation-checkpoint.md).
The verified updater is deployed; WP-01, WP-02, WP-04 and
[WP-03](remediation/2026-10-30/wp-03-verification.md) are independently accepted.
The [WP-05 choices](remediation/2026-10-30/wp-05-decisions.md) await human selection.

## Install and operate

| Guide | Contents |
| --- | --- |
| [Installation](installation.md) | Prerequisites, source build, Docker installation and maintenance |
| [First administrator setup](first-run.md) | Setup without an installation key, TOTP and the accepted decision |
| [HTTPS](https.md) | Local and operator-managed certificates, validation and renewal |
| [Recovery](recovery.md) | Account recovery and database/configuration backup boundaries |
| [Localization](localization.md) | Available languages, translation structure and limitations |
| [Planning timezone and portable JSON](timezones.md) | Installation clock, preserved dates and explicit metadata imports |

## Integrate and develop

| Guide | Contents |
| --- | --- |
| [Development workflow](development-workflow.md) | Canonical document-first policy and proportional implementation process |
| [Architecture](architecture.md) | Current components, trust boundaries and domain model |
| [HTTP API](api.md) | API usage and integration contracts |
| [Complete time history](time-history.md) | Revision-guarded pages, filtered totals, compatibility and bounded complete downloads |
| [OpenAPI contract](contracts/openapi.json) | Machine-readable HTTP contract |
| [Directory integration](directory.md) | LDAP and external directory-source behaviour |
| [Optional integrations](integrations.md) | Mail and calendar integration configuration and limitations |
| [Contributor guide](../CONTRIBUTING.md) | Contributions and repository workflow |
| [Agent instructions](../AGENTS.md) | Document-first decisions, scope, tests and independent review |

## Security and verification

- [Security policy](../SECURITY.md): vulnerability reporting and security expectations.
- [Security review](security-review.md): dated findings, dependency decisions,
  verification evidence and known limitations.

## Planned remediation

The [2026-10-30 remediation programme](remediation/2026-10-30/README.md) is currently
**in progress, now planning WP-05**. The user approved starting with the
separate updater, followed by the ordered work packages. WP-01/WP-02/WP-04/WP-03
have exact-SHA acceptance records; WP-05 decisions D09/D10/D13 await selection.
The programme defines WP-01
through WP-05, the implementation order,
acceptance rules and required independent review. Read its
[baseline](remediation/2026-10-30/baseline.md) and
[work-package test matrix](remediation/2026-10-30/work-packages.md) before proposing
implementation work. Tests listed in that matrix are planned unless an evidence
record explicitly reports otherwise.

[Multi-year planning and Git-update options](multiyear-and-updates.md) records the
initial alternatives. The updater remains a separate request; it is not silently
included in the remediation programme. Proposed choices require user approval.

Return to the [project README](../README.md).

- [Managed Git updates](updater.md) — optional administrator UI and host service.

- [Updater verification](updater-verification.md) — exact reviewed SHA and executed evidence.
