# Public integrity digest: privacy policy decision

Status: **VERIFIED FALSE POSITIVE; POLICY DISPOSITION AWAITING THE USER**.
2026-11-03. This record authorizes no suppression or history rewrite.

An actual full reachable-history patch scan with pinned Gitleaks 8.30.1,
explicit default rules, an empty ignore file and inline allows disabled returned
exit 1 with exactly one finding. The generic-api-key rule matches the public
`api_settings_sha256` JSON field in `.github/security-environment.json`, line 8,
introduced by commit `11382902f96e62bc2aea5ee829596a96d77144d2`.

The value is the SHA-256 of the canonical, typed public environment protection
settings, not a credential. A different agent independently read the environment
through REST API 2022-11-28 and recomputed the digest with `environment_settings`,
`canonical` and `digest`. It matches both the introducing commit and the current
record. No approval or write was performed. The public digest is:

`daaf21593602774b044146cd667270adf0d53e0355625ec8145e4f8b7c45c886`.

Raw detector output remains private. Only finding metadata and this already
public integrity digest are recorded here. Patch scanning is not complete
P01 raw-object, build-context, package or image-layer acceptance.

## Three options

| Option | Benefits | Drawbacks and risks |
| --- | --- | --- |
| **A — narrowly adjudicate this exact verified public digest (recommended)** | Preserve history and default detector rules; retain the finding in evidence; reject every unmatched finding. Validate the exact field, historical source identity and approved value against typed public settings before accepting it as non-secret. | Refines the current zero-finding gate and needs explicit user approval, a bounded implementation plan and negative tests. An imprecise classifier could hide another finding, so no wildcard, path-wide or rule-wide exemption is permitted. |
| **B — change representation and rewrite published branch history** | Removes this detector trigger from the history being distributed without adding a finding classifier. | Requires separate explicit authorization to rewrite history and coordinate references. Changes SHAs, invalidates source-bound evidence and can disrupt existing clones. Renaming the current field alone cannot remove a historical finding. |
| **C — retain the present strict gate and leave privacy acceptance blocked** | No policy or source change; no risk of an overly broad classifier. | WP-05 cannot reach acceptance while the known non-secret finding remains blocking. Other independent work may continue, but no privacy PASS may be claimed. |

If A is selected, freeze the exact adjudication inputs and tests before code.
Keep complete raw reports and classified finding counts; never label the original
detector exit as clean. Missing provenance, another value, field, path, source
object, rule, additional payload or additional finding must still block. Recheck
the full history and all required artifact roots afterward. This is a decision
about one non-secret integrity value, not a vulnerability waiver or authorization
to accept a real credential in source or artifacts.

The request for human disposition follows [AGENTS.md](../../../AGENTS.md)'s rule
that unresolved policy exceptions require explicit approval, and the admitted
[privacy plan](wp-05-implementation.md)'s default-rule/no-ignore requirements.
