# WP-05 confirmed member/milestone regression

Status: **ACTUAL RED; BOUNDED FIX PLAN INDEPENDENTLY ADMITTED**. 2026-11-03.
This is an authorization/compatibility regression exposed by required B06,
separate from the user's unverified employee-name report in the project backlog.

## Evidence and existing contract

`TestPlaywrightMemberMilestoneRegression` uses an isolated PostgreSQL schema,
real TLS HTTP App, real member session and stored employee assignment. Starting
from that member's actual GET, changing only its assigned leaf status/brief gives
preview HTTP200. Applying the exact returned preview gives HTTP403/forbidden.
Canonical comparison proves the only additional change is derived milestone
status; protected fields are unchanged. The earlier save-response normalization
probe is not cited as this RED.
The separate READ-ONLY reviewer checked the actual log/hash, permission comparator
and all successful calculator paths before admitting the narrow correction.

Private log SHA-256:
`f262a0835dc96882b3843eea40a3dbe89bf8c2e2795e053087069ed5025118f2`;
failing fixture SHA-256:
`68c9434b2fa7b1d7eba26f7a12a85164f7be80879f0fca5d159fe94611fbfdc4`.

Current `projects.go` memberChange permits status/brief changes only on assigned
tasks, comparing all other fields. `schedule.go` rollupProject derives summary
status from children and overwrites the submitted summary status. This makes a
derived read-only value in the server's own preview fail its next permission
comparison. The correction must preserve assignment and protected-field rules.

## Bounded correction

Root owns only projects.go/core_test.go for this correction; the frontend writer
retains its Go HTTP regression and browser fixture. No schema/dependency/role/
API field/route change. The independent reviewer remains READ-ONLY.

Identify actual milestones from the STORED parent/child relation, never the
caller's Summary flag or changed hierarchy. Treat only their derived Status as a
read-only comparison field, consistently with existing Actual/Summary handling.
The authoritative calculator must still overwrite it from children before any
write. Do not grant a member direct control of an unowned leaf or milestone.

Preserve nonmutation of the request and all title/brief/effort/assignment/hierarchy/
requested-date/customer/completed-range protections. No blanket task comparison
skip or trust in client-derived flags. A forged summary status must not persist;
forged unowned leaf status or protected fields must still be rejected unchanged.

## Verification and acceptance

Run the exact real HTTP RED to GREEN. Add independent unit oracles for nested
stored milestones, caller-forged Summary on an unowned leaf, protected parent
title/brief/plan and hierarchy, unauthorized leaf status and input nonmutation.
Verify ignored summary status is recalculated in persisted/API data, including a
forged done value. Keep existing role/scope and concurrent-revocation checks
required. Rerun actual B06 and all three full journeys, plus the full relevant
security/API regression on the final SHA. No deployment or acceptance follows
from this plan or a focused green alone.
