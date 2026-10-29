# Bounded remediation programme

Status: **DOCUMENTATION ONLY — NOT IMPLEMENTED**. Date: 2026-10-30.
Owner approval is required for architectural choices before implementation.
This branch does not authorize code changes, migrations, deployment, or a main merge.

## Goal and execution order

Address demonstrated architectural, functional and operational defects without
rewriting the application. Sequence: **WP-01 → WP-02 → WP-04 → WP-03 → WP-05**.
Freeze scope and acceptance before each WP; deliver independently reviewable commits.
No WP currently has an implementation or acceptance result under this programme.

| WP | Priority | Outcome | Status |
| --- | --- | --- | --- |
| WP-01 | P1 | Reproducible multi-year schedules, including2025–2027 | Proposed; calendar policy pending |
| WP-02 | P1 | No stale UI commits; measured read-path performance | Proposed; workload/SLO approval pending |
| WP-04 | P2 | Complete time history and explicit time-zone semantics | Proposed; API/time-zone decisions pending |
| WP-03 | P2 | Bounded frontend modularisation and enforceable checks | Proposed; tools/module boundaries pending |
| WP-05 | P2 | All-image scanning, restore evidence, cross-browser/security gates | Proposed; release policy pending |

Read [baseline](baseline.md), [work packages and test matrix](work-packages.md),
[WP-03 and WP-05](wp-03-05.md), [decision register](decisions.md), and [evidence/review protocol](evidence.md).

## Rules

1. Baseline: record exact commit, test commands/results and known limitations.
2. Document first: approve WP scope, invariants, decision IDs and acceptance matrix.
3. Reproduce: add a deterministic failing regression before fixing a demonstrated defect.
4. Implement only allowed changes. New findings go to the register; do not expand scope silently.
5. Produce evidence from a clean checkout and synthetic isolated data.
6. Have a different reviewer inspect code, requirements and evidence in READ-ONLY mode.
7. Resolve review findings, rerun affected checks, then review the final SHA again.
8. Finish with combined regression, compatibility, recovery and security acceptance.

Do not conflate a passing test suite with correctness of untested scenarios.
Do not label unrun tests as passing, skips as passes, or proposed thresholds as agreed.

## Deferred request: Git updater

The user also requested downloading/installing updates from Git. This remains
tracked separately in [multi-year/update options](../../multiyear-and-updates.md).
It is **not** folded into WP-05 or implemented by this branch. Its architecture,
trusted source/release channel, backup/restore procedure and authorization model
remain unapproved. After this documentation-only instruction, previously presented
questions do not imply permission to implement either choice automatically.

## Definition of final acceptance

Every WP must reach `PASS — INDEPENDENTLY VERIFIED` against its own final SHA.
Then verify the combined release candidate: no stale data, multi-year correctness,
complete time history, accepted timezone policy, required browser matrix,
security gates, migration/restore and reproducible build. No unresolved blocker
may be waived by an implementer. Final deployment is a separate explicitly recorded
operation with a backup and a documented recovery procedure.
