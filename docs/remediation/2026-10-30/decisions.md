# Decision register

All selections below are **PROPOSED / NOT APPROVED** unless explicitly stated.
The user approved the goals, documentation-only work and delivery to a separate
branch; this does not approve implementation designs or numeric thresholds.
Accepted intent: remove the single-year limitation; keep holidays, weekends,
individual schedules and existing data compatible. Update-from-Git remains separate.

Recommendations are proposals, not agent-made architectural decisions.
Before implementation, record the human selection, date, rationale, rejected
alternatives, affected contracts, migration and verification consequences.

| ID | A — benefits; costs/risks | B — benefits; costs/risks | C — benefits; costs/risks | Recommendation / prerequisite |
| --- | --- | --- | --- | --- |
| D01 Multi-year calendar coverage | Explicit immutable calendar version per year: reproducible official dates; blocks if a required year is unavailable | Verified years plus clearly provisional weekday fallback: supports long-range plans; holidays/transfers unknown, requires visible provenance and reviewed replacement | Project-owned multi-year manual calendar: flexible; maintenance and versioning burden, risk of incorrect holidays | B accepted by the user on 2026-10-30; verified calendars replace provisional dates through preview |
| D02 Scheduling bounds | Documented fixed multi-year horizon: predictable resource ceiling; still rejects longer legitimate work | Calendar-coverage-driven horizon with explicit operation/memory budget: accommodates multiple years; budget exhaustion/retry and sparse availability need defined semantics | Background calculation jobs with resource quotas: larger workloads; queues, cancellation, status and consistency significantly expand scope | B accepted by the user on 2026-10-30; explicit budget, no unlimited loop or silent truncation |
| D03 Refresh correctness | Ordered/versioned polling with in-flight deduplication and visibility backoff: small change; bounded freshness delay remains | Conditional requests plus versioned cache: less transfer/work; validators/invalidation contracts must be correct | Server events plus catch-up reads: low update latency; persistent connections and reconnect ordering add complexity | A accepted by the user on 2026-10-30; one active refresh, revision checks and pause while hidden |
| D04 Derived capacity/read path | Keep on-demand calculation, optimize queries and batch reads: simple consistency; repeated CPU cost | Cache by all input revisions: lower warm cost; invalidation across project/calendar/employee inputs is complex | Persist/materialize derived schedules on writes: cheap reads; larger transaction/migration and rebuild contract | A accepted by the user on 2026-10-30; measure first, then reduce queries/repeated work; reference criteria accepted after the first baseline, below |
| D05 Time history pagination | Offset with revision guard: simple page numbers; deep-page costs and edit-triggered restarts | Keyset with revision guard: bounded traversal; an edit explicitly invalidates the cursor instead of pretending it is a retained snapshot | Materialized snapshot with TTL: stable concurrent browsing; retained sensitive data, storage/cleanup/revocation costs | B accepted by the user on 2026-10-30, including 100/500 rows, 15-minute context, milestone descendants and separate full history exports; [packet](wp-04-decisions.md) |
| D06 Timezone ownership | Installation IANA timezone with compatible default: minimal common-day model; teams share one zone | Project IANA timezone: local planning; bounded first version must restrict mixed-zone employee sharing | Employee-specific scheduling timezone: common local employee day; broad interval/date conversion changes | A accepted by the user on 2026-10-30; installation configuration, compatible Moscow policy, preserve historical dates/minutes; [packet](wp-04-decisions.md) |
| D07 Frontend test tooling | Vitest + Testing Library + ESLint + Prettier: fits Vite and component tests; new dependencies/configuration to maintain | Existing Node/CDP harness plus ESLint/Prettier: fewer new runtime tools; component isolation needs manual harnesses | Playwright component tests plus lint/format: real browser rendering; heavier/slower setup and experimental surface must be assessed | A proposed; verify current compatibility/licensing/security before accepting dependency versions |
| D08 Refactor boundaries | Extract approved hooks/pure domain helpers in existing tree: minimal changes; some coupling remains | Introduce feature folders incrementally: clear ownership; wider import/move diff | Replace state management/framework: broad redesign, high migration risk | A; do not bundle mass formatting with behavioural fixes |
| D09 Browser regression | Playwright Chromium/Firefox/WebKit required subset plus existing CDP coverage: shared API; engine/tool versions and native dependencies need pinning | Separate WebDriver harnesses: standards-based coverage; duplicate fixtures/driver maintenance | External browser service: broad environments; cost, credential handling and data-transfer boundary | A; WebKit engine coverage is not a claim of testing actual Safari/iOS |
| D10 Vulnerability/release policy | Block all HIGH/CRITICAL findings: simple rule; upstream unfixable advisories can stop releases | Block HIGH/CRITICAL with time-limited human-reviewed exceptions: explicit ownership/expiry; acceptance discipline required | Reachability-only gating: fewer false alarms; static analysis can miss exploitation paths | B proposed; no implementer-created exceptions, no silent ignore-unfixed |
| D11 Project/fact read coherence | Include fact add/delete in the established exclusive portfolio lock: minimal compatible change; global contention must be measured | Shared project-row locks in explicit read transactions: per-project contention; broader path/order audit | Repeatable-read input snapshot: coherent views; different isolation/retry/freshness semantics | A accepted by the user on 2026-10-30 after actual failing race; end the materialized read transaction before network output; [decision](wp-02-consistency.md) |
| D12 Project export timezone provenance | Keep legacy JSON default and offer optional portable versioned JSON: compatible workflow; two labelled choices | Portable JSON UI default with explicit legacy choice/API: metadata by default; old importers reject new files | ZIP with unchanged project and metadata sidecar: self-contained; new archive parser/security/resource surface | A accepted by the user on 2026-10-30; legacy default remains, optional portable format has explicit version/zone metadata; [analysis](wp-04-decisions.md) |

## Open acceptance parameters

- D02: supported year range, maximum per-task duration, calendar coverage cap,
  maximum calculation operations, load-array size and cancellation deadline.
- D04: test machine resources, number of projects/employees, read/write mix,
  p99/throughput/resource reporting and exact request mix. The user accepted
  the WP-02 numeric profile below; freeze its remaining experimental controls
  before executing the long run.
- D05: cursor expiry, response envelope/backward compatibility, snapshot consistency,
  export completeness and UI pagination controls.
- D06: semantics of date-only work_date, overdue/today, audit instants, exports,
  cross-project capacity day, default for existing records and zone-change effects.
- D10: mandatory severity/source coverage, exception approver, expiry, recheck cadence,
  restore RPO/RTO, supported upgrade origins and browser versions.

## Accepted WP-02 direction

D03 A and D04 A were selected explicitly by the user. D03 keeps bounded polling
and existing identity/authorization checks; it does not introduce server events or
a new cache contract. D04 retains on-demand schedule calculation and its current
correctness rules. Baseline profiling must precede query or repeated-work changes.
Neither selection approves new dependencies, materialized schedules, cross-request
caches. WP-02 starts after WP-01 acceptance and its own bounded implementation plan.

### Accepted WP-02 performance criteria

After the first actual baseline (1280 authenticated reads, zero errors, five-project
ten-user read p95 about 164 ms), the user explicitly selected option A on 2026-10-30:
five projects × 300 tasks, 30 shared employees and ten distinct concurrent users;
read p95 ≤500 ms, preview/save p95 ≤2 seconds, no unexpected errors; ten minutes of
mixed load plus fifteen minutes of soak. The sixty-minute soak remains a final
WP-05 gate. Option B used the same thresholds with a sixty-minute WP-02 soak
(more observation time, slower completion); option C deferred SLO approval and
would have left performance acceptance open. Neither was selected.

Record the reference machine resources, toolchain, seed, request mix/rate, actual
sample counts/throughput, latency definition, expected conflict responses and
resource series before treating the measured results as acceptance. These are
reference-installation targets, not a hardware-independent performance promise.

## Source verification requirements

Runtime versions/features must be checked against current primary documentation
when making the implementation decision; these links are research entry points,
not evidence that an unselected tool is installed or suitable:

- [PostgreSQL isolation](https://www.postgresql.org/docs/current/transaction-iso.html)
- [React effects and cleanup](https://react.dev/reference/react/useEffect)
- [IANA time-zone database](https://www.iana.org/time-zones)
- [ESLint documentation](https://eslint.org/docs/latest/)
- [Prettier documentation](https://prettier.io/docs/)
- [Vitest guide](https://vitest.dev/guide/)
- [Testing Library principles](https://testing-library.com/docs/guiding-principles/)
- [Playwright browsers](https://playwright.dev/docs/browsers)
- [Trivy vulnerability scanning](https://trivy.dev/latest/docs/scanner/vulnerability/)

No package version is selected by this plan. Current accepted runtime contracts
remain effective until a replacement decision is approved.
