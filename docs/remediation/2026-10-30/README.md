# Bounded remediation programme

Status: **WP-01/WP-02/WP-04/WP-03 ACCEPTED; WP-05 BOUNDED PLANNING**. Updated: 2026-10-31.
See the [current checkpoint](../../implementation-checkpoint.md). WP-01 and the
separate Git updater have their own independent verification records.
Owner approval is required for architectural choices before implementation.
The user subsequently authorized implementation beginning with the updater;
pending architecture choices still require approval. The user explicitly resumed implementation. No main merge is authorized.

## Goal and execution order

Address demonstrated architectural, functional and operational defects without
rewriting the application. Sequence: **WP-01 → WP-02 → WP-04 → WP-03 → WP-05**.
Freeze scope and acceptance before each WP; deliver independently reviewable commits.
See [WP-01 evidence and acceptance](wp-01-verification.md).
WP-02 progress and actual baseline failures are in its
[verification record](wp-02-verification.md).

| WP | Priority | Outcome | Status |
| --- | --- | --- | --- |
| WP-01 | P1 | Reproducible multi-year schedules, including 2025–2027 | PASS — INDEPENDENTLY VERIFIED at 5812b2c |
| WP-02 | P1 | No stale UI commits; measured read-path performance | PASS — INDEPENDENTLY VERIFIED at 400ae188 |
| WP-04 | P2 | Complete time history and explicit time-zone semantics | PASS — INDEPENDENTLY VERIFIED at e97971d; [evidence](wp-04-verification.md) |
| WP-03 | P2 | Bounded frontend modularisation and enforceable checks | PASS — INDEPENDENTLY VERIFIED at 9b03c90; [evidence](wp-03-verification.md) |
| WP-05 | P2 | All-image scanning, restore evidence, cross-browser/security gates | D09 A/D10 B/D13 A accepted in [the packet](wp-05-decisions.md); bounded plan before implementation |

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

## Separate Git updater

The updater entry point B (administrator UI and separate host service) was accepted
and implemented before the remediation WPs. It remains outside WP-05. Read the
[verification record](../../updater-verification.md) and [saved checkpoint](../../implementation-checkpoint.md).
Working-instance installation and service configuration are verified after resume. No remediation
WP receives PASS status from the updater's acceptance.

## Definition of final acceptance

Every WP must reach `PASS — INDEPENDENTLY VERIFIED` against its own final SHA.
Then verify the combined release candidate: no stale data, multi-year correctness,
complete time history, accepted timezone policy, required browser matrix,
security gates, migration/restore and reproducible build. No unresolved blocker
may be waived by an implementer. Final deployment is a separate explicitly recorded
operation with a backup and a documented recovery procedure.
