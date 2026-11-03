# D14 A — bounded protected-review implementation

Status: **INDEPENDENTLY ADMITTED IMPLEMENTATION PLAN — EXCEPTION ADMISSION DISABLED**.
2026-11-03. The READ-ONLY reviewer admitted the canonicalization, trust, privacy,
scope and mandatory-result contracts. Implementation and actual setup are not
accepted by this plan review.
The user selected [D14 A](wp-05-approval-proof.md) on 2026-11-02. This plan applies
D10 B, not a particular waiver. Strict scans and all other WP-05 gates remain
mandatory. No agent may submit an approval, bypass protection or add a real waiver.

## Trust and ownership

The trusted repository/workflow and its reviewed verifier are part of the trust
boundary; this does not defend against the owner rewriting the workflow. CI has
only `contents: read` and `actions: read`. Never put an owner credential in CI or
grant `deployments: write`/`administration: write` to it. Reject exception admission
on forks, pull-request workflows, another repository/run/attempt or foreign artifacts.
That restriction concerns NONEMPTY exceptions. Existing strict zero-request PR
checks remain supported: GITHUB_SHA can be a merge commit while REST head_sha is
the PR head. Bind validated scan candidate SHA to trusted GITHUB_SHA, and artifact
head_sha to the live GET-run head_sha separately. Nonempty dispatch requires
exact candidate/head/workflow identity in the expected repository, with no
foreign head repository; a strict fork check cannot authorize a waiver.

Expected public repository ID: 1438943020 (DrToppi/gm-vector); owner reviewer ID:
161593815 (DrToppi). Pin those numeric identities, not a display name alone.

| Writer | Permitted area |
| --- | --- |
| Admission writer, reassigned from recovery after its checkpoints | New `production/scripts/image_admission.py`, `test_image_admission.py` and `image-admission.py` CLI only; no strict scanner edits, real waivers, GitHub writes or workflow edits |
| Root | Producer/privacy integration, runner, production.yml, explicit environment setup/readback, documents and final evidence |
| Independent reviewer | READ-ONLY design, source and synthetic-negative verification; no source fixes or approvals |

## Immutable proposal without rebuilding the approved image

Never commit a new exact app-image waiver and rebuild that candidate: embedding
the changed source SHA would change its image ID. Waiver proposals are separate
data supplied only through an optional manual workflow-dispatch input
`exception_request` (bounded JSON), not a source-code mutation or authorization.
Normal push/PR runs have an empty request.

Each request specifies an exact image role, advisory ID, package/version, target,
reason, compensating controls, expiry and recheck. No wildcard or version range.
Preparation resolves the role to the already-frozen immutable image ID, validates
an exact current finding match and freezes the complete resolved list BEFORE
review. Resolution proposes a scope; only the later owner review authorizes it.
Unrequested new blockers, duplicate/unused requests and UNKNOWN severity block
admission. UNKNOWN needs explicit triage and is never silently waived by D10 B.

No renewal or approval reuse: every new preparation has fresh run-attempt identity,
nonce, reports, image IDs and canonical digest. Maximum exception lifetime is
14 days measured conservatively from trusted bundle issuance, because GitHub's
review API does not supply an authenticated approval timestamp. Recheck and
expiry must still be valid after the human review.

Input is data: pass it through an environment variable/private file, never an
interpolated shell command. Enforce a 64 KiB request and 2 MiB review-bundle budget,
strict object keys/duplicates, bounded counts, timezone-aware UTC timestamps and
static public error messages. Missing/malformed/expired input fails closed.

## Three-job DAG and always-required result

1. Existing verify/prepare job runs all existing tests plus actual three-image
   scan and complete privacy/provenance gates. A finding is not a scanner error:
   require complete validated outputs before proposing any exception. With zero
   requests, a blocking finding fails immediately and never waits for review.
   A valid strict PASS with zero requests also never enters the environment.
2. Only a valid nonempty proposal starts the protected admission job using the
   `security-exceptions` environment. The owner reviews the exact artifact/images/
   findings/list and enters the canonical approval digest in the review comment.
   Download by the exact artifact ID from the same run, never a mutable name or
   another run. Execute only checked-out reviewed verifier code, no artifact scripts.
3. A tiny `if: always()` required final job validates producer success plus either
   strict zero-request PASS or successful protected admission with the matching
   receipt. A skipped/failed/cancelled required exception job cannot yield green.
   GitHub treats skipped required jobs as success, so a conditional job alone is
   insufficient.

Freeze SHA-pinned Node24 actions: upload-artifact v7.0.2
`cf430e030ddbb5b0abf93d22962f4752f3646cd9`, download-artifact v8.0.2
`9000827ccba6bdab643e8b6fd33ac0654aef8333`. Set upload overwrite false,
if-no-files-found error, and download digest-mismatch error. Bind repository ID,
run ID/attempt, candidate/verifier SHA, artifact ID/digest, fresh nonce and every
member hash. Recheck artifact REST metadata, expiry, source SHA and contents after
download; never accept an artifact on its self-declared metadata alone.

Avoid a self-referential hash: upload the frozen bundle first, then compute the
approval digest from its canonical member digest plus the actual upload ID/digest,
trusted run context and nonce. A separate `bind` step exposes that string in the
trusted job summary/outputs without editing the uploaded bundle. The protected
verifier recomputes it independently. Likewise, upload a generated admission
receipt first and pass its resulting immutable ID/digest through trusted job
outputs to the final job; do not put its own future artifact hash inside itself.

## Private raw evidence and public review data

Raw command logs, config/DB material and image archives stay private. Do not
automatically upload raw diagnostic directories. The review artifact contains only
strict allowlisted canonical projections with complete unfiltered package/finding/
resolved-waiver membership and image/scanner/DB/build identities. Strip environment,
history, arbitrary labels, descriptions, user paths and diagnostic text. Preserve
all severity/status/fixed-version information needed for the gate; no suppression.

Trusted preparation validates full raw reports/SBOMs locally and records their
hashes. It also hashes the public projections; the downstream job rehashes those
actual projection bytes and verifies the immutable trusted-producer artifact.
The original raw hashes are provenance references, not a claim that the next job
received or rehashed private raw bytes. Fail preparation if the public projections
cannot represent complete accepted membership or pass privacy checks. Do not
claim persistent private retention of an ephemeral GitHub runner after it ends.

## Actual owner proof and environment setup

The protected job requires an approved review record for exact owner ID and
environment ID with comment `GM_VECTOR_EXCEPTION_APPROVAL_V1 <canonical-digest>`.
The digest includes run attempt and a fresh nonce. The API has no documented
approval timestamp, attempt or bypass flag; do not invent one. A failed-job rerun
with an old prepared bundle must fail, even if an older approval remains visible.

Create the concrete environment with only DrToppi as required reviewer. Sole-owner
runs require self-review prevention off; this proves account authorization, not
physical presence or two-person control. Disable administrator bypass and record
actual readback. Published REST/GraphQL schemas initially omitted the bypass
toggle, but the actual REST2022-11-28 response on 2026-11-03 exposes it. Authorized
PUT false was honored, and a separate independent GET confirmed literal boolean
`can_admins_bypass: false` for environment26111380009. This observed capability
was independently admitted as a procedural refinement, not a new policy.
Require that live field to be exactly false on every admission: missing/null/0/
string/true blocks. Fingerprint it with the other actual protection settings;
never infer absent means false. This concrete installation needs no UI fallback.
Fail closed if the observed API capability disappears. Keep exception admission
held until setup/code verification is complete. The owner review
record is still checked for every concrete exception; a self-declared JSON
approved_by field, setup flag or ordinary Issue comment cannot authorize it.

## Frozen helper entry points and verification

| Command | Frozen inputs and outputs |
| --- | --- |
| `image-admission.py prepare` | Required `--scan-dir`, `--privacy-receipt`, new `--output-dir`; optional private `--request-file`. Validate raw local evidence, produce allowlisted review projections and summary; no review wait for zero requests |
| `image-admission.py bind` | Required `--bundle-dir`, trusted `--artifact-id`/`--artifact-digest`. After upload, verify metadata and expose the canonical approval binding without changing uploaded bytes |
| `image-admission.py approve` | Required downloaded `--bundle-dir`, trusted `--artifact-id`/`--artifact-digest`, checked-out `--environment-config`, new exclusive `--output-file`. Revalidate immutable artifact metadata, projection bytes, exact owner review and policy, then emit a scoped receipt |
| `image-admission.py final` | Required `--bundle-dir`, trusted preparation `--artifact-id`/`--artifact-digest`, `--prepare-result`, `--admission-result`; nonempty path additionally requires `--admission-file`, trusted `--admission-artifact-id`/`--admission-artifact-digest` and `--environment-config`. Revalidate immutable receipt metadata and current owner proof; absent conditional success cannot green a request |

Trusted run context comes from standard current GitHub runner variables, not a
downloaded context file or self-declared artifact. Unit helpers accept explicit
synthetic context objects. Runtime API credentials come only from GITHUB_TOKEN;
never an argument or public file. API base is fixed to api.github.com with TLS
verification, 10-second calls, bounded response/pagination and no write endpoint.
No partial check is full acceptance. Root wires artifact IDs/digests through
trusted job outputs, never through an artifact's own assertions.

Required units use synthetic records for missing/foreign/swapped artifacts,
edited/missing hashes, wrong repo/owner/environment, stale attempt/nonce/comment,
expiry >14 days, stale/future DB, malformed/duplicate/oversized input, wildcard/
unused/overlapping waiver, uncovered blocker, UNKNOWN, changed package/target/image,
API failure/pagination limits, bypass without owner proof, zero-request no-wait,
missing/skipped/cancelled conditional job and private markers in public projection.
Actual strict CI and environment setup/readback are separate required evidence;
selecting the mechanism does not establish any approved vulnerability exception.

The helper-only slice was independently reviewed at `3a22fc3` on 2026-11-03:
31 pure tests passed independently. A reproduced private-endpoint leak in package
identities was closed by rejecting recognized private IP literals in every public
projection string, preserving exact membership rather than redacting it. Ordinary
version and PURL positive controls pass. Workflow upload/download, the real
read-only CI token's environment visibility and required-final integration remain
unverified; this scoped result does not enable a waiver or establish WP-05
acceptance. No concrete exception or approval event has been created.

## Frozen contract appendix

Canonical bytes C(value) are UTF-8 JSON, sorted object keys, ensure_ascii false,
separators comma/colon, no whitespace, no NaN/Infinity and no duplicate keys.
Files contain C(value) plus exactly one LF. IDs are decimal strings, not floating
point numbers. Hashes are lowercase SHA-256; normalize artifact digests to
`sha256:<64 hex>`. No array deduplication may lose repeated packages/findings.

The public bundle has exactly eight files: manifest.json, waivers.json, and
app/proxy/postgres each with .report.json and .inventory.json. The seven payload
files use formats gm-vector-review-report-v1, gm-vector-review-inventory-v1 and
gm-vector-resolved-waivers-v1. Manifest format is gm-vector-review-bundle-v1; it
contains trusted repository/run/attempt/candidate/verifier context, issued_at,
64-hex fresh nonce, scanner/DB identity, three image identities, raw report/SBOM/
archive/build/privacy hash references, strict status and size/hash membership for
the other seven files. It does not hash itself.

Each report projection preserves every validated raw result's target/class/type,
every package identity/name/version/release/epoch/PURL and every vulnerability
ID/package ID/name/path/installed version/fixed version/severity/Status. Missing
optional raw fields are represented as null, not silently reclassified. Preserve
row multiplicity and validate projections against RAW rows; the scanner summary
alone omits Status and is insufficient. Inventory projections preserve all
component identity/type/name/version/PURL and dependency/finding membership,
without raw environment/history/labels/descriptions. Human request reason,
compensating controls and recheck remain verbatim in waivers: reject unsafe or
sensitive text rather than silently changing reviewed scope. CVE descriptions
are diagnostic data and may be omitted; owner rationale may not.

Bundle digest is SHA256(`gm-vector-review-bundle-v1` + NUL + C(descriptors)), where
descriptors is the filename-sorted list of name/size/SHA256 for ALL eight actual
files, including manifest.json. Approval digest is SHA256(`gm-vector-approval-v1`
+ NUL + C(binding)); binding contains those trusted context fields, nonce,
bundle_digest and actual artifact ID/digest obtained AFTER upload. Thus no digest
depends on an artifact field that would need to contain its own future digest.
The review comment is exactly `GM_VECTOR_EXCEPTION_APPROVAL_V1 <approval-digest>`.

Limits: request64 KiB, whole public bundle2 MiB, exactly8 regular files, exactly3
images; at most16 result groups/image, 20000 package or finding rows/image,
100 resolved requests, 1024 bytes for an identity/path, 2048 UTF-8 bytes each for
nonblank reason/controls; API2 MiB/response, at most10 pages of100 reviews/artifacts,
10-second request deadline. UTC timestamps must include a timezone; validate raw
DB freshness at preparation AND after review. Expiry/recheck are after issuance,
recheck no later than expiry, expiry at most issuance+14 days. Reject unknown
fields, symlinks, extra/missing members, truncation, unsafe strings and ambiguity.

Manual workflow-dispatch input is PUBLIC review data, not a secret channel.
Document that credentials, internal addresses/customer details are forbidden.
Env/private-file transport prevents shell injection; it does not make input secret.

The checked-out environment setup record pins numeric environment/reviewer/repo
IDs and the canonical digest of actual reviewer/self-review/admin-bypass/wait/
branch settings. `bypass_disable_verification` has method api_readback, UTC
verified_at and evidence_sha256. It must match the live environment and literal
false bypass field. No absent field is false. Unfinished setup blocks the
exception path only; the record is a prerequisite and can NEVER substitute for
a concrete owner review record.

Final invariants: prepare-result MUST be success. Zero requests require raw
strict PASS, admission-result skipped and no receipt flags. Nonempty requests
require admission-result success plus exact verified receipt/artifact/proof.
Never relabel the raw scanner's BLOCKED as PASS: use a distinct
ADMITTED_WITH_EXCEPTIONS result and retain the original strict finding state.

## Checked primary sources

- [Environment protection/UI bypass controls](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments).
- [Environment REST schema](https://docs.github.com/en/rest/deployments/environments).
- [Run approval history and read permissions](https://docs.github.com/en/rest/actions/workflow-runs).
- [Artifact metadata](https://docs.github.com/en/rest/actions/artifacts).
- [Conditional/skipped required jobs](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-jobs-with-conditions).
- [Pinned upload action](https://github.com/actions/upload-artifact/tree/v7.0.2) and
  [pinned download action](https://github.com/actions/download-artifact/tree/v8.0.2).

Read-only source checks supplied version/capability facts at initial admission.
The concrete environment was subsequently created and verified as described
above. No approval event or individual exception has been created.
