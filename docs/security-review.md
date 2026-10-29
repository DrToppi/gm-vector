# Initial public snapshot review

Date: 2026-10-29. Scope: the initial GM Vector source snapshot and its locally built
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
