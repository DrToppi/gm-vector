# gmVector documentation

Start with the operator guides for the current application. Architecture proposals
and remediation plans are labelled separately; they are not promises that the
corresponding functionality is implemented.

Current work is [resumed from the documented checkpoint](implementation-checkpoint.md).
The verified updater is deployed; WP-01 and WP-02 are independently accepted.
WP-04 is next under its [bounded plan](remediation/2026-10-30/wp-04-implementation.md).

## Install and operate

| Guide | Contents |
| --- | --- |
| [Installation](installation.md) | Prerequisites, source build, Docker installation and maintenance |
| [First administrator setup](first-run.md) | Setup without an installation key, TOTP and the accepted decision |
| [HTTPS](https.md) | Local and operator-managed certificates, validation and renewal |
| [Recovery](recovery.md) | Account recovery and database/configuration backup boundaries |
| [Localization](localization.md) | Available languages, translation structure and limitations |

## Integrate and develop

| Guide | Contents |
| --- | --- |
| [Development workflow](development-workflow.md) | Canonical document-first policy and proportional implementation process |
| [Architecture](architecture.md) | Current components, trust boundaries and domain model |
| [HTTP API](api.md) | API usage and integration contracts |
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
**in progress, now preparing WP-04**. The user approved starting with the
separate updater, followed by the ordered work packages. WP-01/WP-02 have exact-SHA
acceptance records; D05 B/D06 A/D12 A are accepted for WP-04. It defines WP-01
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
