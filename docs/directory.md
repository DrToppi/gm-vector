# Directory and external-reference contract

GM Vector provides provider-neutral REST integration, scoped API tokens and
direct LDAP login with explicit synchronization. An explicitly inactive identity
disables bound accounts and revokes sessions and tokens; missing records are
reported without changing access.

## API and identities

`GET /api/v1/directory/people?q=` searches local authoritative snapshots (admin or
`directory:read`). `POST /api/v1/directory/sync` accepts {provider,entries:[{id,name,email,
username,active,groups}]} (admin or `directory:write`), maximum 1000 atomic entries.
It cannot create accounts, assign roles or send invitations. Source namespace
and external ID are identity; email/name are never used to merge identities.
`GET /api/v1/directory/status` and `POST /api/v1/directory/ldap/sync` are admin-cookie only;
LDAP secrets remain local in the file referenced by runtime `ldap_file`.

Project `external_reference` is a manager-controlled string up to 500 bytes.
The fresh database baseline uses this neutral field. Older private snapshot keys are not accepted by the public import API.

## Invitations and login

Local employees: email explicitly supplied by an administrator. External employees:
email comes only from trusted source snapshot, rechecked before invitation and
before SMTP send. Changes revoke pending invitations. Import never creates user
accounts. LDAP accounts are created by explicit invitation; the corporate password
is checked during TOTP enrollment and never stored. Local password+TOTP remains
for local users. No local-password fallback for LDAP; recovery cannot create one.

The configured LDAP source uses a read-only search account, TLS certificate and
hostname verification (LDAPS or mandatory StartTLS), escaped filters, bounded
search/time limits. Stable ID mapping supports text entryUUID or binary
objectGUID encoded base64url. Groups are metadata only, never automatic roles.
User login searches fresh identity/state and binds the supplied nonempty password.
If that search explicitly reports the account inactive, existing sessions and API
tokens are revoked immediately. A missing record or connection failure denies
the attempted login without changing other account state. Scheduled synchronization
is not enabled automatically; administrators invoke synchronization explicitly.
Directory outage denies LDAP login and does not disable local administrators.

Explicit inactive entries revoke bound accounts/sessions/API tokens; absent
entries are counted but unchanged. Historical projects and time entries remain.
Active=true does not automatically re-enable an account disabled earlier: an
administrator reviews access. Privileged local recovery identities cannot be
converted to LDAP by synchronization. No writes back to the directory.

## Sources and verification

Checked 2026-10-29: https://www.rfc-editor.org/rfc/rfc4513.html (Bind/StartTLS),
https://www.rfc-editor.org/rfc/rfc4515.html (filter encoding),
https://pkg.go.dev/github.com/go-ldap/ldap/v3 (v3.4.15, MIT, latest module query).
Tests use synthetic TLS/users and isolated PostgreSQL; no production directory.
Cover scope separation, duplicate identity, email provenance, atomic sync,
TLS/password rejection, LDAP enrollment, explicit deactivation and migration.

## LDAP configuration

Set runtime `ldap_file` to a protected (0600) JSON file mounted as a secret. Never commit the populated file. Example with synthetic placeholders:

```json
{
  "enabled": true,
  "url": "ldaps://directory.example.test:636",
  "provider": "corporate",
  "bind_dn": "cn=reader,dc=example,dc=test",
  "bind_password": "REPLACE_OUTSIDE_REPOSITORY",
  "base_dn": "ou=people,dc=example,dc=test",
  "filter": "(objectClass=person)",
  "id_attribute": "entryUUID",
  "name_attribute": "displayName",
  "email_attribute": "mail",
  "username_attribute": "uid",
  "active_attribute": "enabled",
  "active_value": "TRUE",
  "groups_attribute": "memberOf",
  "max_entries": 1000
}
```

For AD use `objectGUID` with `binary_id: true`, `sAMAccountName`, and `active_attribute: "userAccountControl", disabled_bit: 2` instead of `active_value`. Corporate CA PEM may be specified by `ca_file`. Plain `ldap://` requires `start_tls: true`; there is no insecure-certificate option. The search is bounded to 1000 entries and aborts without applying partial results on directory size/time limits. Narrow `base_dn` / `filter` to the participating population if the upstream limit is lower. The deployment administrator restarts the service after config changes; web status exposes only configured/enabled/provider.

An administrator synchronizes explicitly, then invites an employee using the existing SMTP invitation flow. LDAP users keep their directory username and verify their corporate password before setting up mandatory TOTP. Password hashes are not created for LDAP users. Local break-glass administrators remain independent from directory lifecycle. There is no automatic group-to-role mapping.

## Explicit employee source selection

Accepted user clarification, 2026-10-29: employee lookup must offer **LDAP** or
**API integration with a service desk**, with a provider selector for multiple API
sources. Manual local employees remain a separate operation. Source and stable
external ID remain attached to the employee; selection never merges identities
by email or display name. The configured LDAP namespace cannot be overwritten by
API synchronization. Public API exposes source metadata and filters; there is no
built-in dependency on any service-desk vendor.

The people endpoint accepts `source=ldap` or `source=api`, and optional exact `provider`; omitted filters return all active sources. `directory/status.source_details` returns `{provider,kind}` alongside the backward-compatible `sources` string list. The configured LDAP provider is reserved and cannot be overwritten by the external sync endpoint. Manual administrator-managed employees are separate from either connector.

The public release uses a fresh database baseline and accepts neutral project JSON only. Imports deliberately clear external references to prevent accidentally linking a duplicate project to an upstream issue. Review and set the reference after import. Earlier private installations require a separate offline conversion of exported project files; do not apply this baseline to an existing private database or rewrite its migration journal.
