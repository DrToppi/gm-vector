# Contributing to GM Vector

Start with an issue describing the problem or proposed change. For larger changes,
agree on the API, data migration, and acceptance criteria before implementation.

Fork the repository, create a focused branch, and submit a pull request. Include
what changed and how it was tested. Use synthetic fixtures and an isolated test
database; never run test suites against a real workspace.

Keep API changes documented in OpenAPI and the integration guide. Preserve access
checks, transaction boundaries, optimistic revisions, and secret redaction.
Translations must preserve parameters and plural forms. Do not send private task
content to translation services.

Contributions are reviewed by maintainers. No direct push access is needed to
submit a contribution. Confirm that you have the right to submit the contribution
under the project's Apache-2.0 terms; preserve third-party attribution.
