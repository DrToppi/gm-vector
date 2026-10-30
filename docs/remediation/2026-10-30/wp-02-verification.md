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
The coherence and slow-response hypotheses were then actually reproduced with
barriers under race: GET returned old revision 0 with newly committed 15 minutes
(stored revision 1), and a capacity writer waited on a database Lock while a
fully materialized response was held. See the
[D11 options and required decision](wp-02-consistency.md). No production correction
to locking/isolation is claimed yet.

## Remaining work

Implement and verify selection/session/revision/resource guards, one background
refresh batch, editor/write/visibility/offline pause and catch-up, plus actual
read-path comparisons before/after optimization. Reproduce backend hypotheses
before calling them defects. Freeze the remaining profile controls, run the
agreed duration/mix, then independent exact-SHA review and final CI. Preserve
WP-01 behaviour and all prior identity/security tests.

## Committed implementation checkpoints

- D03 source `40885e3fddabdc42228e04a841886f46e2d666f7`: selection/operation
  generations, monotonic project revisions with current equal-revision derived
  updates, shared catalog ordering, foreground priority and one completion timer.
  The writer ran coordinator units and 17 actual browser cases, seven-locale build,
  prior identity units/browser suite and actual API/PostgreSQL E2E (20.70 seconds).
  A different read-only reviewer independently ran all 17 cases on that exact
  committed frontend source. No D03 blocker remained in that review.
- Review first reproduced two additional regressions in the initial fix: a stale
  child callback adopting the new selection and an archived project reopening
  from the old sidebar list. Both gained real held-response tests and passed
  independently after correction. Foreground catalog/open overlap and reconnect
  without a loaded project are also covered.
- Backend source `20bc3b5b77a2685fd7fd86edcc7390dc249db196`: accepted D11 A,
  materialized reads released before network output, and annual-version reuse
  limited to one prepare/transaction with independent composite maps. API/race,
  calendar-reuse and D11 add/delete/cancellation/slow-response tests passed as a
  focused batch (17.612 seconds). These are mandatory integration runner checks,
  not opt-in-skipped acceptance claims.

The reviewer also independently executed the exact20bc D11/cache race suite:
9.340 seconds, PASS. It covered add/delete, cancelled writers, a cancelled held
reader without partial DTO/pool leakage, and GET/preview/export release before
slow output. This remains component evidence, not full WP-02 acceptance.

## Comparable profiling replay

The identical final test harness (SHA-256
`68a234025609c39ac610c7266632c3d8e5746c9c6ef4577bc28c3573285b624e`)
was run sequentially without competing heavy jobs against baseline runtime
`d9320469f4d0d4e0aaf866db2e2bfb391a1ca274` and candidate
`20bc3b5b77a2685fd7fd86edcc7390dc249db196`. The isolated baseline checkout
received only that specified test file; its production files were unchanged.
Both runs completed 1280 requests with zero errors and unchanged persisted digests.

| Five projects, 3000 facts/project, ten users | Baseline | Candidate |
| --- | --- | --- |
| Total SQL calls/read | 48 | 40 |
| Annual-calendar SQL calls/read | 10 | 2 |
| Read p50, 200 samples (ms) | 151.602 | 116.959 |
| Read p95 (ms) | 183.600 | 164.576 |
| Read p99 (ms) | 196.481 | 172.639 |
| Prepare mean, five phase samples (ms) | 52.9704 | 46.3486 |

The query reduction is deterministic. Tail latency is an observation from one
short pair, not a universal speedup: the single-project p95 varied upward in the
candidate (0 facts: 52.632→74.781 ms; 3000 facts: 45.683→51.554 ms). All short
read cohorts remained below 500 ms, but mixed writes and required durations have
not passed merely from these samples. Allocation figures include both test client
and server. No global cache, database index or schema change was introduced.

Private raw replay evidence SHA-256:

- Baseline: `0e08ba6b0f3b4c597de36e1e9fa82e6e038abb3cf5449507962da71531d0f635`.
- Candidate: `9746bc4c94e92e346ed974c501f65c89b27464159a95b9da09a4eaba067699c4`.
- Normalized comparison: `33a2fec5af153913a788600b6ee1dee188c6fab02854e2cefd310356cab44531`.

## CI infrastructure follow-up

A later documentation-only CI again reached the multiyear browser's six-second
startup deadline with Chrome still alive and no DevTools port. Sanitized DBus
messages were present, but their causal role is unknown. The fixture startup
budget is now aligned to the existing main/identity harness standard of fifteen
seconds, with actual readiness elapsed time and a >6000 ms marker recorded.
Spawn/exit failures remain immediate; there is no restart, sandbox bypass or
change to application assertions, functional waits or API latency SLOs. The local
affected scenario passed with 202 ms readiness. Keep the original failed evidence;
do not claim the unknown DBus/startup cause was diagnosed.

The agreed long mixed/soak run, final combined regression, exact-SHA independent
review and final CI remain open.
