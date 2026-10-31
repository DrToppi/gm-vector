# Working on gmVector

These instructions apply to this repository. Read the user's current request,
[documentation index](docs/README.md), the
[development workflow](docs/development-workflow.md), and the relevant decision and work-package
documents before editing. A request to document a change does not authorize its
implementation or deployment.

## Current work mode

The user explicitly resumed work. The verified updater was deployed and its host
service enabled; WP-01 is independently accepted at 5812b2c, WP-02 at 400ae188 and
WP-04 at e97971d. WP-03 implementation follows under accepted D07 A/D08 A. Its
tool versions, rule inventory, symbol map and measured repair appendix passed a
READ-ONLY plan gate in docs/remediation/2026-10-30/wp-03-implementation.md.
Mechanical formatting is separate from bounded lint repairs and extraction.
See [the updated checkpoint](docs/implementation-checkpoint.md).

The user approved implementation beginning with the Git updater, followed by
the remediation programme on 2026-10-30. The updater entry point is option B
(administrator UI and separate host service); see docs/updater.md. Unresolved
architectural decisions still require human approval. Its scope and ordering are recorded in
[the programme README](docs/remediation/2026-10-30/README.md).
Do not change application code, dependencies, schemas, workflows, running services
or installation data as part of a documentation-only task. Do not mark planned
tests as executed or work packages as implemented.

## Document-first decisions

Apply the canonical [development workflow](docs/development-workflow.md).
Keep the process proportional: a small, low-risk correction within accepted scope
does not require a new architecture proposal or repeated authorization. Material
architectural changes, unresolved requirements and exceptions to approved scope
require explicit human resolution before implementation.

- Architectural decisions belong to the user. Record requirements, invariants,
  allowed changes, compatibility concerns and acceptance criteria before code.
- For a new architectural choice, present exactly three viable alternatives with
  benefits, drawbacks and risks. Check current primary documentation, link sources,
  and record the user's explicit selection. A recommendation is not approval.
- Preserve already accepted decisions. If new evidence requires a change, describe
  the conflict and obtain approval before implementing a different architecture.
- Treat approved documentation as the intended contract, but verify it and prior
  agent reports against current code and evidence. Resolve contradictions before
  implementation; neither an old report nor current code silently overrides the
  accepted requirements.
- Keep observed findings, hypotheses, proposed thresholds and verified results
  separate. Do not claim production readiness from a subset of passing tests.
- Keep public documentation and repository descriptions in English. Product text
  changes must account for all supported locale catalogs.

## Scope and coordination

The remediation order is **WP-01 → WP-02 → WP-04 → WP-03 → WP-05**. Before each
package, freeze its bounded plan, test matrix, approved decisions and permitted
files or modules. Additional findings belong in the documented backlog, not an
unannounced expansion of the current package.

When the user requests parallel agents, an orchestrator assigns non-overlapping
file ownership and a registrar records findings, decisions, verification and
outstanding work. Keep one writer per file. Agents report their scope and actual
results to the orchestrator; they must not overwrite another agent's work.
An independent reviewer has a READ-ONLY role and must not fix the implementation
being reviewed. Do not imply independence merely by renaming the implementer.

Work on a task branch. Preserve unrelated local changes and user data. Commit
coherent changes separately, describe what was verified, and push only to the
agreed repository and branch. Do not merge into main, publish a release or deploy
unless the user has authorized that action. Existing authorization within the
current task remains valid; do not ask for it again without a concrete reason.

## Verification and evidence

Use `production/scripts/verify.py` as the entry point for existing unit,
integration and browser checks. Read its prerequisites before running it.
Integration and E2E checks require a disposable database named
`gma_planner_test`; never point tests at an actual workspace. Use synthetic
projects, identities, mail servers and directory fixtures. Do not send real mail
or contact a configured external integration as a test.

Select checks appropriate to the change and complete required gates. For a bug,
first establish a deterministic regression that demonstrates the failure. Check
boundary, invalid-input, concurrency and compatibility cases where relevant.
Use independent expected values instead of duplicating implementation logic in
tests. For documentation-only changes, check consistency, scope, references,
links, formatting and accidental private content; do not run destructive or
unrelated application tests just to produce a green label.

Record the exact source SHA, commands, environment, fixture seeds, exit codes,
skips, results, limitations and sanitized evidence locations. Benchmarks must
identify the agreed workload and machine, with reproducible before/after data.
A skipped or unrun check is not a pass. A scan result is dated evidence, not a
promise that no vulnerabilities exist.

A work package may receive **PASS — INDEPENDENTLY VERIFIED** only after a different
reviewer checks the requirements, code, compatibility and evidence for the exact
final implementation SHA. A later implementation change invalidates that result
for the changed revision and requires review again. Documentation review alone
cannot confer implementation acceptance. Final operational acceptance also
requires the combined regression, security and restore gates in the programme.

## Security and data boundaries

Never commit credentials, tokens, private keys, populated configuration, customer
projects, local installation paths, private logs, database files or diagnostic
archives. Inspect changes and artifacts for accidental private content before
publication. Use neutral examples and synthetic fixtures. Do not read unrelated
secrets or live customer data to prepare public documentation.

Preserve authentication, TOTP, role checks, scoped API tokens, CSRF/origin checks,
TLS verification and database privilege boundaries. Do not disable a protection
or suppress a failing security check to make a test pass. Review security-sensitive
changes and their failure behaviour explicitly.

Deployments and migrations require an authorized plan, a matching backup of data
and configuration, and a tested recovery procedure. Never delete user volumes or
assume that starting an old image reverses database migrations. Keep signing and
backup keys separate from distributable artifacts.
