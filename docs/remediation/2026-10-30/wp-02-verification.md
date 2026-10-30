# WP-02 verification record

Status: **IMPLEMENTATION IN PROGRESS — NOT ACCEPTED**.
Decisions [D03 A/D04 A](decisions.md) and the
[bounded contract](wp-02-implementation.md) govern this package. Work starts from
`d932046` on `feature/ordered-refresh`, after independently accepted WP-01.
No deployment or main merge is part of this record.

## Demonstrated baseline defects

Before frontend runtime edits, the new synthetic loopback browser harness actually
exercised the current React Workspace. Its assertions failed on these outcomes:

| Controlled delivery | Expected | Observed baseline |
| --- | --- | --- |
| Hold old A; select and load B; release A | B remains selected | A replaces B |
| Hold old A; select B then a new A; release old A | Latest A remains | Obsolete A replaces Latest A |
| Return an old same-revision derived response after the current response | Current ready capacity remains | Old blocked_capacity replaces ready |
| Hold a project poll; accept a newer list employee catalog; release the old poll | Fresh employee catalog remains | Old employee replaces Fresh |

The corrected catalog-specific fixture fails at the actual stale-value assertion;
an earlier catalog selector timed out and is not used as proof of that defect.
These are executed application failures, not compilation failures or assumptions
from reading the old setInterval code. A separate read-only reviewer independently
reproduced A→B→A with the actual bundled React/Chrome baseline.

Reproduction commands (the baseline is expected to exit 1):

```sh
node production/scripts/workspace-refresh-check.cjs
node production/scripts/workspace-refresh-check.cjs --case shared_catalog
node production/scripts/workspace-refresh-check.cjs --case repeated_selection
```

Use held JSON bodies and explicit request barriers. Catalog fixtures and profiles
contain synthetic data only. No running browser, customer workspace, live calendar,
mail server or directory is used. Private raw evidence is retained outside Git.

## Backend baseline

The test-only ready-portfolio profile is being prepared. Query counts, timing
distributions and coherence/slow-consumer hypotheses have not yet been measured
or accepted. The historical opt-in stress test is not a WP-02 performance gate.
No numeric SLO has been approved; do not infer one from engineering experiments.

## Remaining work

Implement and verify selection/session/revision/resource guards, one background
refresh batch, editor/write/visibility/offline pause and catch-up, plus actual
read-path baseline measurements before optimization. Reproduce backend hypotheses
before calling them defects. Obtain explicit numeric performance acceptance, run
the agreed profile, then independent exact-SHA review and final CI. Preserve
WP-01 behaviour and all prior identity/security tests.
