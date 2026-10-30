# WP-03 implementation choices

Status: **PROPOSED; D07/D08 NOT SELECTED**. Prepared while WP-04 implementation
runs. No WP-03 dependencies, configuration or application changes are authorized
by this document. Start only after WP-04 acceptance and the user's selections.

## D07 — frontend component checks and tools

ESLint and Prettier were requested already. The decision here selects component
infrastructure and its maintenance cost, without replacing existing browser/API
regressions.

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — Vitest, React Testing Library and a DOM environment; ESLint/Prettier | Fits the existing Vite/TypeScript application; concise observable component tests and controlled request/timer fixtures | Additional development dependencies and DOM shims; native layout/gestures still need real browsers | Mocked DOM/API checks cannot prove layout or backend security; pin compatible versions, audit the lockfile and keep real browser/API gates |
| B — extend current Node/CDP component fixtures; ESLint/Prettier | Reuses the working real React/Chrome fixtures and avoids a separate component runner | More custom test plumbing, selectors and lifecycle helpers to maintain | Home-grown waits can reintroduce stale-DOM races; require readiness barriers and explicit cleanup/failure checks |
| C — Playwright Test with a small component story gallery; ESLint/Prettier | Real browser layout and shared interaction/trace tooling; potential reuse in WP-05 | Adds browser binaries, native prerequisites and a gallery served by the existing dev server | Test fixtures must remain isolated from shipped routes/data; pin supported tooling, retain API integration evidence and assess CI cost |

Recommendation: **A** for a focused component layer, retaining all established
browser suites. The official current Playwright documentation now describes
framework-agnostic gallery tests with `@playwright/test`; the former experimental
component packages have been removed. Do not install a retired package based on
an older plan.

For any selected option, proposed shared tooling contract: ESLint flat config for
all authored web TypeScript/TSX and new component tests, TypeScript-aware correctness
rules and React hook correctness; zero unexplained warnings. No broad suppression
to manufacture success. Prettier covers authored frontend sources/configuration,
excluding generated assets/catalogs, backend and vendored files. Mechanical
formatting gets a separate commit before semantic extraction. CI runs non-mutating
lint/format checks. An existing finding must be repaired within scope or explicitly
reported; never silently turn off a rule. Freeze the final rule/ignore inventory
and exact dependency versions after selection and initial measured findings.

## D08 — module boundaries

All three options preserve React, API contracts, D03 ordering, existing state
semantics and the seven locales. A new state framework is excluded from this WP,
so the earlier rewrite alternative is replaced by an in-scope option.

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — focused hooks, pure helpers and named view components in the existing tree | Smallest import/file-move diff; separates workspace coordination, editor state and rendering incrementally | Existing file layout remains partly flat | Hook closure/lifecycle regressions; use the accepted race/draft tests and a symbol-move manifest |
| B — incremental feature folders for workspace, tasks/history, people and charts | Clear feature ownership and public entry points | Wider import/move diff; more barrel/boundary choices to review | Circular dependencies or accidental API exposure; forbid cycles and keep shared types/transport explicitly owned |
| C — presentation-first extraction, retain one workspace controller | Low state-transition risk; separate task table, header/actions and dialog composition from rendering | Central controller remains relatively large; less reduction in coordination coupling | Technical debt remains visible; constrain scope honestly instead of claiming full controller decomposition |

Recommendation: **A**, freezing a symbol-level module map after WP-04 code is
accepted. Do not prematurely move code that the current writers are changing.
No arbitrary line-count target or unrelated redesign is part of this choice.

## Current primary references

Checked on 2026-10-30 against current official documentation, not recalled package
APIs. Exact installed versions/security must still be checked at implementation:

- [Vitest setup and current engine requirements](https://vitest.dev/guide/).
- [React Testing Library and its DOM dependency](https://testing-library.com/docs/react-testing-library/intro/).
- [ESLint flat configuration](https://eslint.org/docs/latest/use/configure/configuration-files).
- [Prettier non-mutating CLI checks](https://prettier.io/docs/cli).
- [Current Playwright component gallery testing](https://playwright.dev/docs/test-components).

The installed reference environment uses Node 24 and Vite 7. Package installation
must verify actual lockfile engines/peers rather than assume every future latest
version is compatible. No test framework selection constitutes a test PASS.
