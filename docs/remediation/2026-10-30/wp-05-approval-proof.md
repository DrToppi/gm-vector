# D14 — evidence of vulnerability-exception approval

Status: **PROPOSED — HUMAN SELECTION REQUIRED**. Checked on 2026-10-31.
D10 B already allows narrowly scoped exceptions for at most 14 days, approved
only by the owner. It does not choose how the gate authenticates that approval.
A JSON field saying approved_by is self-declared and cannot prove authorization.
No particular advisory exception has been approved.

## Three alternatives

| Choice | Benefits | Drawbacks | Risks |
| --- | --- | --- | --- |
| A — GitHub protected environment `security-exceptions`, owner review before the exception job proceeds | Uses the existing repository identity and review UI; no additional private signing key | Requires repository environment configuration and an online approval step only when exceptions exist | Owner credentials and trusted workflow/settings must be protected; review must bind immutable artifact/image/report/waiver digests, not an arbitrary replacement file |
| B — offline Ed25519-signed exception bundle, pinned maintainer public key | Verifiable without a live approval API; exact content is cryptographically bound | Separate protected private key, signing procedure, public-key rotation and CI bundle delivery | Lost/compromised signing key and key-replacement trust must be managed; signature does not replace expiry/scope checks |
| C — explicit owner Issue comment containing the canonical waiver digest, verified through GitHub API | Familiar text approval and visible history; no additional private key | Depends on comment availability, API access/rate limits and a strict content parser | Edited/deleted/foreign comments must fail validation; owner-account compromise remains a risk |

Recommendation **A**, not approval. This environment is a CI security gate and
does not deploy the application. Normal runs with no exceptions must never wait
for manual approval. Blocked findings remain blocked until a concrete scoped
exception is separately approved; selecting D14 is not that approval.

For A, require only the configured owner reviewer, disable administrator bypass
and verify the actual environment settings. Because the repository has a sole
maintainer who triggers runs, Prevent self-review cannot be enabled for those
runs without adding another human reviewer. Keeping it off allows that account
and its authorized credentials to approve; do not call this physical human
presence or independent two-person approval. CI retains read-only permissions and
cannot approve its own job. No approval is automatically submitted by an agent.

The workflow must freeze candidate SHA plus image IDs and immutable report/SBOM/
waiver artifact hashes before review, then verify those same hashes afterward.
Rebuild/replacement, new findings, expiry or stale advisory data invalidate the
admission. The GitHub approval record alone does not bind a different payload.
Do not weaken the ordinary strict scan or reuse an approval for another image.

All alternatives retain D10's exact advisory/package/version/image/target scope,
reason, compensating controls and recheck; maximum 14-day lifetime; no wildcards,
automatic renewal, ignore-unfixed or reachability waiver. B must authenticate the
pinned key and signature; C must verify exact repository/owner identity and current
comment contents. Approval failure/unavailability stays a blocking result.

## Independent progress

Strict scans, freshness/identity validation, complete reports, negative controls,
browser and recovery work do not need a waiver and may proceed under the accepted
WP-05 plan. Exception admission remains disabled until this decision is selected
and its bounded verification design is reviewed. No environment, approval Issue,
signing key or repository protection setting has been created by this proposal.

## Current primary references

- [GitHub environment protection and reviewers](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments).
- [Reviewing and rejecting protected jobs](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/review-deployments).
- [Workflow review/approval API](https://docs.github.com/en/rest/actions/workflow-runs).
- [Issue comment API](https://docs.github.com/en/rest/issues/comments).
- [Ed25519 signing/verification primitives](https://cryptography.io/en/latest/hazmat/primitives/asymmetric/ed25519/).

These describe available mechanisms; they do not establish that a particular
review, signature or exception exists. Record the user's selection before
implementing or configuring exception admission.
