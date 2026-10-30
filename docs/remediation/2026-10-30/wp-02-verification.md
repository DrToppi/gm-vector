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

The first test-only ready-portfolio profile actually ran with production Go
unchanged from d932046: 1280 authenticated loopback-TLS GETs, zero errors and
unchanged persisted project digests. Fixtures had one/five projects × 300 tasks,
30 employees, 600 loads/project, two selected years and zero/3000 time facts per
project. Ten-user cohorts each contained 200 samples:

| Projects | Facts/project | Read p95 (ms) | SQL calls/request |
| --- | --- | --- | --- |
| 1 | 0 | 61.251 | 16 |
| 1 | 3000 | 50.566 | 16 |
| 5 | 0 | 149.758 | 48 |
| 5 | 3000 | 164.067 | 48 |

The harness also ran one/five-user cohorts, component phases and actual synthetic
query plans. Repeated two-year calendar reads and validation occur for each of
five relevant projects. Baseline allocations are for the combined in-process
client/server, not server-only memory. pgx acquisition duration includes all pool
acquisition time; do not call it pure queue wait. Phase warmup precedes HTTP reads,
so first_completed_ms is not a cold-cache measurement. No hardware/cache flushing
is claimed. Reference workstation: 12 available logical CPUs, GOMAXPROCS 12,
about 31.3 GiB RAM, Go 1.27.3, PostgreSQL through the disposable local test database.
TLS, authentication and authorization are exercised; nginx and external networking
are excluded from these API latency figures. The historical opt-in stress harness
is not a WP-02 performance gate.

The user subsequently accepted reference SLO option A: five projects, 30 shared
employees, ten distinct concurrent users; read p95 ≤500 ms, preview/save p95
≤2 seconds and zero unexpected errors; ten minutes of mixed load plus fifteen
minutes of soak. The sixty-minute soak remains in final WP-05. The short baseline
above is not proof that these duration/mixed-write acceptance criteria passed.
Coherence and slow-response hypotheses still require controlled reproduction.

## Remaining work

Implement and verify selection/session/revision/resource guards, one background
refresh batch, editor/write/visibility/offline pause and catch-up, plus actual
read-path comparisons before/after optimization. Reproduce backend hypotheses
before calling them defects. Freeze the remaining profile controls, run the
agreed duration/mix, then independent exact-SHA review and final CI. Preserve
WP-01 behaviour and all prior identity/security tests.
