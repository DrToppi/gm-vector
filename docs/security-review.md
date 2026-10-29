# Initial public snapshot review

Date: 2026-10-29. Scope: the initial gmVector source snapshot and its locally built
container/offline distribution. This is a publication-readiness review, not a
claim that every possible application vulnerability has been eliminated.

## Publication boundary

The repository was assembled from an explicit source/documentation allowlist into
a new directory and new Git history. Historical projects, deployment configuration,
private documentation, screenshots, archives, Git history, local launcher scripts,
credentials and keys were excluded. Commit metadata uses a project identity.

Independent manual review covered identifiers, URLs, email addresses, filesystem
paths, Unicode names, configuration examples and synthetic fixtures. Two tests
reused a name example from prior development discussion; those were replaced with
explicitly synthetic fixture identities before publication.

## Scanning

- Gitleaks8.30.1 scanned the staged source snapshot with redaction enabled: no
  unresolved secret findings. A public RFC6238 TOTP test vector was rewritten as
  an explicit encoding of its published test bytes to avoid resembling a live key.
- Product-source/catalog/HTML and built JavaScript string-literal checks found no
  former provider affiliation. Minifier-generated variable names are not product text.
- Distribution checks inspect all OCI image layers, including files removed in a
  later layer, for private installation filenames, environment files and private
  PEM bodies. Image and package manifests contain hashes and public metadata only.
- No real installation files or customer data were read to build the snapshot.

## Functional and security checks

The clean source passes the unit suite with Go race detection, API/database
integration tests, role/scope checks, directory lifecycle tests, verified-TLS LDAP
and SMTP fixtures, and browser E2E against a disposable database. Browser coverage
includes first administrator setup, QR/manual TOTP, seven language choices, persisted
profile locale, project editing, scheduling, chart interaction, exports, invitations,
encrypted report persistence and the public GitHub draft privacy boundary.

The role matrix covers357 endpoint/role cases. Separate restore checks verify
encrypted backups, unprivileged runtime access, network-isolated recovery and
secret exclusion. HTTPS tests cover certificate/key mismatch, CA and hostname
verification, proxy-header overwriting, and ACME configuration parsing; no public
ACME certificate was requested during these tests.

The disposable Docker installer check also passes: empty workspace, verified HTTPS,
administrator/TOTP setup, repeated preparation without changing secrets, local
recovery and session/token revocation. Recovery links never appear in installer
stdout/stderr. All35 layers across the app, database and proxy images were reviewed;
no private installation files or complete private key bodies were found.

## Deployment limitations

LDAP was exercised against a synthetic TLS directory, not a live corporate AD.
Translations are complete catalogs but have not undergone professional native
language review. Detailed backend validation messages and server mail are outside
the browser translation catalogs. The calendar provider adapter remains prepared
but live calendar OAuth/synchronization is not enabled. See the corresponding guides.

New installations use the neutral schema. Never point this release directly at an
older private installation; migrate via reviewed exports into a new workspace.

## Proxy dependency follow-up — 2026-10-30

Updated chi/v5 from 5.3.2 to 5.4.0 (GHSA-j6qf-3m9c-vwj3) and
go-jose/v4 from 4.1.5 to 4.1.6 (GHSA-mgcc-cww5-fp2q).

Before proxy replacement, three advisories remained open for go-jose/v3 3.0.5: GHSA-mgcc-cww5-fp2q,
GHSA-4w3g-jfj2-cw2q and GHSA-vvm8-gf78-28g4. Upstream lists no patched
v3 release. This dependency is compiled into Caddy through its internal PKI,
smallstep/certificates and go.step.sm/crypto/jose; it is not merely a test
dependency. The latest crypto 0.91.1 still requires v3.0.5. A passing
call-graph scan does not remove these package-level findings.

References: [RSA JWK advisory](https://github.com/advisories/GHSA-mgcc-cww5-fp2q),
[CBC-HMAC advisory](https://github.com/advisories/GHSA-4w3g-jfj2-cw2q),
[JSON advisory](https://github.com/advisories/GHSA-vvm8-gf78-28g4),
[crypto release](https://github.com/smallstep/crypto/releases/tag/v0.91.1).

### Decision — option B accepted on 2026-10-30

| Option | Benefits | Costs and risks |
| --- | --- | --- |
| A: retain Caddy pending an upstream fix | No deployment change; retains internal TLS, supplied certificates and ACME. | Vulnerable code remains in the binary; requires exposure analysis and continuing monitoring. Do not present this as fixed. |
| B: replace the bundled proxy with maintained nginx, supplying certificates separately | Removes this dependency chain without owning a cryptographic fork. | Changes packaging, header handling and certificate lifecycle; internal CA and automatic ACME need separate provisioning and regression tests. |
| C: maintain a reviewed patch/backport of the affected dependency chain | Can preserve Caddy behavior while addressing the vulnerable operations. | Own security-critical fork, provenance and update burden; a module-path replacement alone is unsafe because v3/v4 APIs differ. |

The user approved option B: replace Caddy with nginx and manage certificates
separately. The old proxy Go module and binary are removed from new builds.
The preceding 2026-10-29 results describe the initial snapshot, not validation of
this replacement. Replacement local verification passed: unit/race, API and role matrix, real
nginx per-client/account throttling, browser E2E, TLS certificate/header checks,
and empty Docker installation with repeated preparation and local recovery.
The new archive contains 36 OCI layers; no forbidden private files or complete
private keys were detected. GitHub CI is an independent publication gate.
No vulnerability alerts are dismissed.

The nginx 1.30.6 Alpine base required updates to brotli-libs, curl and libcurl.
The Dockerfile applies those available security updates. A fresh Trivy scan of
the resulting proxy image found zero known vulnerabilities on 2026-10-30; the
HTTPS regression passed again. CI now rejects HIGH/CRITICAL proxy-image findings.
This is a dated scan result, not a guarantee against future advisories.
