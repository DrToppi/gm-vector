# Contributing to GM Vector

Start with an issue describing the problem or proposed change. For larger changes,
agree on the API, data migration, and acceptance criteria before implementation.
Follow the [document-first workflow](docs/development-workflow.md).

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

## Frontend checks

Use compatible Node 24 (at least 24.15.0 for the pinned DOM test environment).
From `production/web`, install the lockfile with `npm ci --ignore-scripts` and run:

```sh
npm run check
npm run check:tests
npm run lint
npm run format:check
npm run test:components
npm run test:i18n
npm run build
```

`npm run format` writes formatting changes; keep them separate from behaviour
changes. Generated locale catalogs and the lockfile are excluded from formatting.
The component tests use real session/request guards with synthetic fetch fixtures;
they do not establish native layout, browser focus, API authorization or scheduling
correctness. Keep the real browser and API/PostgreSQL checks.

The unified `production/scripts/verify.py all` requires a disposable database named
`gma_planner_test`, Chromium and native nginx. It also runs the frontend gates,
module-cycle/boundary checks and deliberate negative controls. Use `--dsn-file`
for a private test DSN and `--nginx-bin` if nginx is outside PATH; never commit
the DSN. `e2e --only-browser-api` is an explicitly focused diagnostic, not full
acceptance. See the [bounded frontend plan](docs/remediation/2026-10-30/wp-03-implementation.md)
and [actual evidence](docs/remediation/2026-10-30/wp-03-verification.md).
