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
