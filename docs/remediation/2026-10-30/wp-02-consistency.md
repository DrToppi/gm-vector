# WP-02 project/fact consistency decision

Status: **D11 A ACCEPTED BY THE USER ON 2026-10-30**.
This is a demonstrated consistency defect, not a reason to replace the scheduler.

## Executed reproduction

The isolated race-enabled API test held the time aggregate read after the project
header had been read. Another actual API request committed 15 minutes and changed
the stored revision from 0 to 1. The original GET returned revision 0 together with
the newly committed 15 minutes: neither the old snapshot (0/0) nor the new snapshot
(1/15). The assertion failed as intended. No working database was used.

Time-entry add/delete lock their project row, but do not participate in the shared/
exclusive portfolio lock used by the other mutable calculation inputs. Under the
current READ COMMITTED transaction, the separate header and aggregate statements
can therefore observe different committed states.

A separate controlled probe materialized JSON and held ResponseWriter.Write. An
otherwise ready capacity writer was observed in PostgreSQL's Lock wait state until
the response was released. The lock currently outlives the protected calculation.
Do not hold that transaction across a potentially slow network write after the
result has been fully materialized. Verify release using a database barrier, not
an elapsed-time guess; preserve the materialized response and error behaviour.

## Exactly three consistency alternatives

| Choice | Benefits | Drawbacks | Risks and verification |
| --- | --- | --- | --- |
| A — include fact add/delete in the existing exclusive portfolio lock, acquired before project row locks | Smallest change; preserves the established lock model and isolation level; header, actual totals and revision remain aligned | Fact writes serialize against calculations across projects | Increased contention; release calculation transactions before response writes, measure the accepted mixed-write SLO, verify lock order, cancellation, add/delete races and preview/export compatibility |
| B — hold project-row shared locks for input reads inside existing transactions | Facts serialize within their project; unrelated projects can continue | More read-path changes and transaction/ordering audit; a shared row lock outside an explicit transaction would not protect later statements | Partial coverage or a new lock cycle; test every read/prepare/export path and existing row-lock order with concurrent writers |
| C — use repeatable-read snapshots for coherent read/preview transactions | A coherent transaction snapshot without globally serializing fact writers | Changes isolation semantics and transaction construction beyond the current lock convention | A snapshot can predate a lock wait; review retry/error, input freshness and authorization boundaries carefully, including writes racing with preview |

The user explicitly selected **A**, paired with ending the materialized read
transaction before network output. Acquire the existing exclusive portfolio lock
before each time-entry add/delete project's row lock, preserving the established
global-before-row ordering. Keep READ COMMITTED and the existing API shapes.
Verify add and delete interleavings, revision/actual alignment, cancellation and
preview/export compatibility. The held writer test must now prove that a slow
network write cannot retain the materialized read's portfolio transaction.
Measure the accepted mixed-write profile rather than assuming contention is cheap.
Options B/C are not selected; do not change the isolation level or introduce a
second read-lock model as part of this correction.

## Sources checked on 2026-10-30

- [PostgreSQL transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html):
  statement-level views under READ COMMITTED and repeatable transaction snapshots.
- [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html):
  row/advisory locks, transaction lifetime and deadlock considerations.

Actual reproduction and code inspection establish this application's defect. The
sources explain available mechanisms; they do not substitute for testing the
chosen correction or imply that a recommendation is approved.
