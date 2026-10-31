# WP-03 verification record

Status: **IN PROGRESS — NO IMPLEMENTATION ACCEPTANCE**.
See [bounded plan](wp-03-implementation.md). Baseline: accepted WP-04 e97971d,
documentation cc19e2a, plan dd0e6f2, branch `feature/frontend-modules`.

## Pre-change observations (2026-10-30)

- Actual baseline `npm run build` exited 0: Vite 7.3.7, 105 transformed modules,
  reported bundling 1.80 seconds. This is one observation, not a performance SLO.
- Baseline JS 687182 bytes / gzip 239485 bytes; CSS 23278 / gzip 6028.
  These gzip values use Python gzip with mtime=0; Vite's displayed compressor
  values differ. Compare before/after with the same method.
- All twelve proposed dev pins/engines/peers/MIT declarations were independently
  verified against exact public npm manifests before installation. No lock/audit
  or test PASS is implied by metadata.

## Phase 1 fixture compatibility

Replaced only the workspace test harness's exact-text insertion with TypeScript
AST instrumentation. It targets one Workspace/controller function, requires the
three expected symbols and a unique top-level return, and requires exactly one
target in the completed test bundle. It is never imported by production code.

Actual executed checks, exit 0:

- `node production/scripts/workspace-fixture-source.cjs`: original and planned
  module names, changed whitespace, invalid/missing/ambiguous targets.
- `node production/scripts/workspace-refresh-check.cjs`: existing coordinator
  unit checks plus all 17 existing browser scenarios, with assertions retained.

## Tooling and mechanical checkpoint

Tooling commit db40db5: 12 exact dev pins, original production resolutions retained;
`npm ls --all` exits 0 with no problems, all 149 installed engine constraints match,
`npm audit` exits 0 with zero findings. The selected lint rules initially reject
25 exhaustive-deps errors; both typechecks reject the single browser-timer/Node-type
conflict. The component command rejects an empty suite. These red baseline results
are preserved in the [reviewed repair appendix](wp-03-implementation.md).

Mechanical commit fb2829e changes 48 frontend files. Actual format/localization and
all 17 workspace browser checks pass; normalized emitted-JS and type-node ASTs
match across 40 TS/TSX files. The explicitly reviewed normalizations join adjacent
JSX literal children, remove redundant parenthesized types and standalone empty
statements; they do not ignore arbitrary whitespace/string differences. Private
normalization result SHA-256:
`0f22e48bfa91d6348529bf3d64a3bb4f8d959f4fc43d33466c7f12c20b976f8b`.
Prettier required a second pass for i18n.ts; final non-mutating check passed. The
known timer type error remains, deliberately outside the mechanical commit.

Actual negative controls (`node production/scripts/frontend-tooling-check.cjs`)
exit 0: every authored TS/TSX file receives correctness/hook rules; each temporary
lint, formatting and component defect makes its own command exit 1 for the expected
reason; probes are removed. The initial sandbox attempt could not spawn a child
Node process (EPERM); the authorized rerun executed the controls. The new frontend
commands are required by the unified unit/all runner and therefore existing CI.

Rule repairs, real component scenarios, extraction, native gesture/keyboard
additions, full runner, exact-source CI and final independent review remain
pending. No merge, deployment or operational readiness is claimed.

## Repair and native-input checkpoints (2026-10-31)

Repair commit 0029f1f changes 16 source files within the reviewed appendix: all
25 lifecycle findings plus the type-only DOM timer and formatter/iterator
refinements. No lint suppression was introduced. Actual lint, both typechecks and
localization pass. The unified E2E attempt also completed frontend build and all
existing workspace/history/multi-year/updater/session browser regressions before
the new real-API native scenario failed; that overall attempt is **FAIL**, not an
API/E2E acceptance.

New native fixture failures were investigated independently of runtime repairs:

1. Port dragging opened the real link editor, but the initial CDP Enter lacked a
   text payload and did not produce the intended native submit. Adding `text` and
   `unmodifiedText` plus an actual submit-event assertion allowed link preview/save.
   [Chromium's protocol definition](https://raw.githubusercontent.com/ChromeDevTools/devtools-protocol/master/pdl/domains/Input.pdl)
   documents those keyboard fields; no application submit bypass was added.
2. After traversing a long modal by Tab, the header cross was outside its scrolled
   viewport. Scroll-to-view plus an actual elementFromPoint assertion made the
   native click close the dialog. The test still dispatches pointer input.
3. The unchanged focus-return assertion then **still failed** after the dialog
   closed. Task Enter, draft-retaining Escape, Tab/Shift-Tab containment, visible
   computed outline and all preceding native gesture/link cases executed first.
   This is the actual 03-09 defect under repair; its historical origin is unproven.

The independently reviewed Modal layout-cleanup correction is documented before
code in the [bounded appendix](wp-03-implementation.md). Preserve the existing
focus assertion; no full E2E PASS until it and subsequent scenarios execute.
Private logs SHA-256:

- Full pre-extraction E2E attempt: `5a41fe86954df89b692baa5251a2b94b739300def105471237a9ea69e0d1d947`.
- Isolated native focus RED: `9cb2b30ce61a89f111a409ec6eeb0f1c00dfe737770d9f1275b3553348200953`.

The runner now offers explicitly labelled `e2e --only-browser-api` for focused
investigation. It still builds frontend and requires the actual named Go test
PASS on the disposable database; it cannot be used with all/unit/integration or
silently pass a skip. Thirteen runner-guard unit tests actually passed, including
the new scope/database/build/evidence controls. The full all/e2e default retains
all established regressions. Final exact-SHA checks remain required.
