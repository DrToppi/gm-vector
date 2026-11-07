# Saved checkpoint — 2026-11-07

Status: **SAVED AT THE USER'S REQUEST; WP-05 REMAINS IN PROGRESS**.
This save does not establish operational acceptance or authorize a deployment,
release, main merge, vulnerability waiver or privacy-policy exception.

## Source and work in progress

Branch: `feature/operational-hardening`. The preceding published checkpoint is
`b25836f`. Commit `8df049c` preserves the remaining browser focus diagnostics:
predicate names, bounded visibility/control metadata and the diagnostic adjustment
for native date-input hosts. It is a saved test-harness change, not a newly
verified application correction.

The private CI proposal has been copied out of temporary storage into a durable
private checkpoint with its review notes, generator and static validator. It has
not been applied to `.github/workflows/production.yml`. Raw diagnostic logs,
browser receipts and scanner evidence are preserved separately from the public
repository, with a SHA-256 inventory and private file permissions. A local Git
bundle preserves committed source and references.

## Verification performed for this save

- Browser TypeScript check: passed.
- ESLint for the changed browser fixture: passed.
- Prettier for the changed browser fixture: passed.
- Gitleaks scan of that fixture: passed with no findings.
- Git whitespace check: passed.

No native browser journey, full regression, image build, recovery exercise or
load benchmark was rerun for this save. The earlier full three-engine run passed,
but the later stronger oracle failed Chromium/WebKit B05 while Firefox passed.
The saved diagnostic-only Chromium run also ended with a failure. Those later
failures must not be replaced by the earlier green result. Finish the date-input
predicate diagnosis and rerun the final native assertions before acceptance.
The earlier generic WebKit server diagnostic remains unclassified.

## Required continuation

1. Resolve the [public integrity-digest finding](wp-05-public-digest-finding.md)
   with the user. No policy option has been accepted; do not suppress the finding.
2. Finish native focus/error-inventory checks and review their exact final source.
3. Review and apply the private three-job CI proposal, verify the real read-only
   token's capabilities, and implement the final-load wrapper. These remain open.
4. Produce actual clean-source image/privacy evidence, run final-candidate recovery
   and all three supported-origin upgrades, combined regression/security and the
   approved 600+3600-second reference load. Obtain independent final acceptance.
5. Keep [BUG-001](../../backlog.md) open: the reported employee-name omission in
   project API data still needs endpoint identification, reproduction and contract
   comparison. No employee-name fix was made by this checkpoint.

For earlier executed checks and their limits, see the
[WP-05 evidence record](wp-05-verification.md) and its linked bounded plans.
