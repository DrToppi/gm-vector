# WP-03 bounded implementation plan

Status: **PHASE 1 PLAN INDEPENDENTLY REVIEWED — NO APPLICATION CHANGES YET**.
The user selected D07 A and D08 A. This plan applies those decisions after
WP-04 acceptance at `e97971ddf5125b433f27e194b1be3144c3748d67`, with documentation
closure `cc19e2a`. Work branch: `feature/frontend-modules`; no merge or deployment.
See [choices](wp-03-decisions.md), [acceptance matrix](wp-03-05.md) and
[workflow](../../development-workflow.md).

## Contract and exclusions

Separate lifecycle coordination, presentation, focused editors and participation
helpers in the existing tree. Preserve API contracts, D03 ordering and immutable
render-bound intents, permission checks, session isolation, clock/history/export
semantics, translations, DOM structure, gestures and editor draft ownership.
No effect dependency or callback identity change is incidental to extraction.

No server/schema changes, runtime dependency upgrades, chart redesign, global state
framework, lazy loading, bundler replacement or unrelated cleanup. `Chart.tsx` and
`Panels.tsx` are not decomposition targets. Formatting alone may touch authored
frontend files; it must be reviewed separately from semantic edits.

## Tools verified before installation

Public npm package manifests were read on 2026-10-30, without installation. The
reference uses Node 24.21.0, Vite 7.3.7, React/DOM 19.3.1 and TypeScript 5.9.3.
Exact proposed development pins (all MIT) are:

| Purpose | Package versions |
| --- | --- |
| Component checks | `vitest@5.0.5`, `@testing-library/react@16.3.3`, `@testing-library/dom@10.4.2`, `jsdom@30.1.3` |
| Lint | `eslint@10.14.0`, `@eslint/js@10.0.1`, `typescript-eslint@8.72.1`, `eslint-plugin-react-hooks@7.1.1`, `globals@17.13.0` |
| Formatting | `prettier@3.9.10`, `eslint-config-prettier@10.1.8` |
| Test/config types | `@types/node@24.19.2` (Node 24 line, not overall latest Node 26 types) |

Vitest accepts Vite 6.4/7/8. The selected TypeScript ESLint and Hooks versions
support ESLint 10; the former accepts TypeScript >=4.8.4 <6.1.0. The strictest
selected Node 24 engine is jsdom's >=24.15.0; reference 24.21.0 satisfies it.
CI/build tools must use compatible Node 24; do not ignore engine/peer failures.
Registry metadata is not proof of a resolved lockfile, audit or executed tests.
Install exact dev pins only, review the lockfile, and retain current runtime
dependency resolutions. New coverage/browser plugins are outside this initial set.

Primary references: [npm Vitest manifest](https://registry.npmjs.org/vitest/latest),
[Vitest](https://vitest.dev/guide/),
[RTL](https://testing-library.com/docs/react-testing-library/intro/),
[TypeScript ESLint compatibility](https://typescript-eslint.io/users/dependency-versions/),
[React Hooks lint rules](https://react.dev/reference/eslint-plugin-react-hooks),
[ESLint flat config](https://eslint.org/docs/latest/use/configure/configuration-files),
[Prettier checks](https://prettier.io/docs/cli). Record the actually resolved
versions and engine/peer checks in the verification receipt.

## Rules, file inventory and preliminary measurement gate

Use explicit, named correctness rules rather than adopting a moving recommended
preset without review. Lint all authored `src/**/*.{ts,tsx}` and new `tests` plus
authored tool configuration; generated locale catalogs and `dist/node_modules`
are excluded. TypeScript remains the authority for undefined names/type errors.
Freeze the complete enabled rule/options inventory below before repairs.
The core includes duplicate/unreachable/unsafe JS constructs, supported
TypeScript correctness counterparts, `rules-of-hooks` and `exhaustive-deps`.
React Compiler adoption and a wholesale ban on existing transport `any` are not
part of D07. Do not enable then silently disable a failing rule to claim success.

After independent plan review, phase 1 may install/configure development tools and
measure existing findings without modifying application behaviour. Record every
rule/count before repairs. A baseline diagnostic is not a clean lint result.
For lifecycle findings, first compare actual closure semantics and race evidence.
Do not auto-add dependencies, reconnect timers or stabilize callbacks blindly.
Any necessary exception must be narrow, explained and independently reviewed;
material contract conflicts or scope expansion return to the user before repair.
The final gate uses zero warnings and reports unused suppression comments.

Prettier covers authored web TS/TSX/CSS/HTML/JSON and configuration, except generated
catalogs, package lock, `dist/node_modules`, backend and vendor files. Use checked-in
options and non-mutating `--check` in CI. Formatting fixes form a separate mechanical
commit before extraction. New tests live under `web/tests`; explicitly typecheck
test/config files, not just the existing `src` tsconfig include. Use jsdom cleanup,
controlled deferred promises/timers, and failing-console/unhandled-rejection checks.

## Symbol and import map

All application paths below are relative to `production/web/src`.

| Current symbols | Destination / boundary |
| --- | --- |
| `App`, authentication/logout/bootstrap | Remain `main.tsx` |
| Workspace state, refs, polling and actions | `useWorkspaceController.ts`: keep `refreshLists`, `loadProject`, `open`, `propose`, `saveGesture`, `commit`, `undo`, `archiveProject`, task handlers and export closures together; retain render-bound intent capture |
| Workspace render/derived view data | `Workspace.tsx`, consumes controller result; no duplicate state or extra effect |
| `CreateProject` | `CreateProject.tsx` |
| `Modal`, `TaskEditor`, `LinkEditor`, `ProjectEditor` | Separate named files; child modules import the leaf `Modal.tsx` directly |
| `PeoplePanel`, `ActivityEditor`, `DirectorySearch`, `ParticipationDetails` | Separate named files; `Editors.tsx` remains a compatibility re-export barrel |
| `weekdays`, reactive `weekdayNames`, `emptyActivity`, `hasParticipation`, `activityCapacity`, `participationLimit` | `participation.ts`, imported directly by task/participation modules; no barrel cycles |
| Existing `Modal` imports in Bug Report/Panels | Narrow import adaptation only; no decomposition |

No production test globals. The existing workspace browser fixture injects hooks
using the fragile string ` const leaves=` in `main.tsx`. Before formatting, replace
that mechanism with TypeScript AST instrumentation at the Workspace/controller
function return boundary. Match expected symbols exactly and fail closed if the
target is absent/ambiguous; keep existing held-request assertions unchanged.
Other fixture import boundaries must be checked after extraction, not deleted.

## Work ownership and gates

1. Orchestrator owns this plan, evidence, CI/runner and fixture instrumentation.
   Independent reviewer remains READ-ONLY. Freeze plan before phase 1.
2. Tooling writer owns web package/lock, lint/format/test configurations and test
   setup. Measure rules, report findings and freeze the appendix before repairs.
3. A single formatting writer formats all allowed web files in a dedicated commit;
   other web writers pause. Run typecheck/build, locale and existing fixture checks.
4. A single semantic writer owns the coupled module extractions above. A separate
   test writer owns `web/tests`; coordinate exported props/imports before touching
   shared files. Root alone wires mandatory runner/CI commands. No concurrent edits
   to the same file and no dependency/source changes by the READ-ONLY reviewer.
5. Freeze final SHA, run full relevant regression and exact-source CI, then obtain
   independent code/requirements/evidence review. A later source fix requires a
   new review; documentation alone cannot confer implementation acceptance.

## Required evidence

On 2026-10-30 a different READ-ONLY reviewer permitted phase 1 only: exact tooling,
diagnostic inventory and fail-closed fixture instrumentation. Runtime/effect fixes
require the measured per-finding appendix first. This is a plan gate, not a WP PASS.

Map every row 03-01 through 03-14 in [the matrix](wp-03-05.md) to an executed check,
an explicit limitation or an unresolved blocker. Required component cases include:

- Workspace A→B→A, obsolete callbacks, held archive/list and session/timer teardown.
- Task title/plan drafts across actual-hours refresh, failed save/cancel/retry,
  busy-close and role restrictions; no cancellation mutation.
- Weekday quotas, date exceptions, invalid activity windows and failed-preview
  input preservation; helper boundary values independent of implementation math.
- History stale results/restart/former employee and all print-window cleanup.
- All seven locales, escaped long user text and keyboard dialog behaviour.

Keep real Chromium/API/PostgreSQL regressions for native dialog, Gantt/map gestures,
export/download/print, authentication, authorization, first setup/TOTP/invitations/
recovery/Bug Report. DOM-only components cannot establish these contracts.
No live external integration, mail or actual workspace is used.

Existing browser checks prove actual Gantt resize, map modes/camera/fold/Trace and
selected milestone styling. They do not yet prove successful bar move, dependency
port drag/edit/cycle, actual wheel/background/node drag or native Tab/Escape/focus
return. Add those targeted cases instead of claiming the existing suites already
cover every row. Component cancellation events cannot establish native dialog focus.

Prove lint, format and component gates each fail for a deliberate disposable defect
and recover after removal; verify every authored file is covered. Include a module
graph/cycle check, symbol-move manifest and request/props comparison. Compare bundle
sizes and representative critical interactions on the same fixture before/after;
report actual differences without inventing a numeric acceptance budget.

Commands must include locked install, typecheck, lint, format check, component run,
build, localization, unchanged prior browser checks and unified full runner. Capture
exact SHA/toolchain, fixture seeds, skips, outputs/exit codes and private evidence
hashes. Scan staged changes/history for private data before pushing. No main merge,
release, deployment or operational acceptance is authorized by this package.

## Initial lint inventory (frozen before diagnostic run)

All rules below are errors. No recommended preset adds implicit rules. Use the
TypeScript ESLint parser for TS/TSX; this initial selection does not require a
type-aware program. Explicit application and test/config `tsc` projects supply
typechecking; do not enable projectService's broad default-project escape hatch.

- Core: `constructor-super`, `for-direction`, `getter-return`,
  `no-async-promise-executor`, `no-class-assign`, `no-compare-neg-zero`,
  `no-cond-assign`, `no-const-assign`, `no-constant-binary-expression`,
  `no-debugger`, `no-dupe-args`, `no-dupe-class-members`, `no-dupe-else-if`,
  `no-dupe-keys`, `no-duplicate-case`, `no-ex-assign`, `no-fallthrough`,
  `no-func-assign`, `no-import-assign`, `no-invalid-regexp`,
  `no-irregular-whitespace`, `no-loss-of-precision`,
  `no-new-native-nonconstructor`, `no-obj-calls`, `no-self-assign`,
  `no-setter-return`, `no-sparse-arrays`, `no-this-before-super`,
  `no-unexpected-multiline`, `no-unreachable`, `no-unsafe-finally`,
  `no-unsafe-negation`, `no-unsafe-optional-chaining`,
  `no-useless-backreference`, `require-yield`, `use-isnan`, `valid-typeof`.
- TypeScript: `@typescript-eslint/no-array-constructor`,
  `@typescript-eslint/no-duplicate-enum-values`,
  `@typescript-eslint/no-misused-new`,
  `@typescript-eslint/no-non-null-asserted-optional-chain`,
  `@typescript-eslint/no-unsafe-declaration-merging`,
  `@typescript-eslint/no-wrapper-object-types`.
- React: `react-hooks/rules-of-hooks`, `react-hooks/exhaustive-deps`.
- `reportUnusedDisableDirectives: error`; command `--max-warnings=0`.

Use default rule options unless the measured inventory explicitly records a
reviewed refinement. `no-undef` is not enabled on TS (compiler owns it); transport
`any`, unused-symbol/style rules and React Compiler presets are intentionally
outside the initial selected correctness set, not disabled after a red run.
Formatting options: Prettier defaults, explicit `endOfLine: lf`; no semantic fix.

## Follow-up carried into WP-05

A separate detailed scan at WP-04 source e97971d identified the generic required-
module advisory as [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932): unmaintained
`golang.org/x/crypto/openpgp` family, all versions, no known fixed version. The
resolved module was x/crypto v0.58.0. Reachable/imported-package counts were zero;
the affected OpenPGP packages were not imported. The module itself is used (for
example Argon2), so calling x/crypto unused would be incorrect. This is a dated
follow-up, not an identifier supplied by the earlier default CI log, a waiver,
or a claim that every dependency is vulnerability-free. WP-05 retains policy and
all-image scanning decisions.
