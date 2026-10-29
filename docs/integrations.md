# Optional integrations

Integrations are operator-controlled. No secrets are shipped in the repository,
and installation does not call third-party services.

## SMTP invitations and task messages

Create `state/config/smtp.json` with mode0600, outside Git:

```json
{
  "enabled": true,
  "host": "mail.example.test",
  "port": 587,
  "security": "starttls",
  "from": "planner@example.test",
  "username": "REPLACE_LOCALLY",
  "password": "REPLACE_LOCALLY"
}
```

Use `tls` for implicit TLS or `starttls` for mandatory STARTTLS. Certificates and
hostnames are verified. For a corporate CA, place `smtp-ca.pem` next to the file
and add `"ca_file": "/run/secrets/smtp-ca.pem"`. Set `smtp_file` in runtime
`state/config/config.json` to `/run/secrets/smtp.json`, then run installer `prepare`
with the existing state directory to copy secrets and recreate services.

An administrator invites an employee. External identities use the trusted
directory email; local employees use an administrator-supplied email. The link is
single-use and time-limited. Enrollment requires a password (corporate password
for LDAP accounts) and TOTP. Existing accounts are not silently merged by email.

Task messages are internal by default. Sending a customer message requires the
author to select the explicit email option. Delivery uses an encrypted outbox;
ambiguous SMTP outcomes are marked uncertain rather than blindly duplicated.

## Calendar providers

Working calendars and calendar-provider adapters are separate concepts.
`GET/POST /api/v1/calendars` manages workdays used by the scheduler. The shipped
Russia2026 five-day calendar can be replaced with an administrator-imported JSON
calendar. Calendar days still include pauses; work does not proceed on weekends.

The internal calendar adapter contract reads busy intervals and publishes/deletes
application-owned busy blocks. Microsoft Graph adapter code and synthetic tests
are retained in `production/internal/planner/graph.go`. OAuth uses verified HTTPS;
deletion must match the recorded operation identity and remote ETag. It never
creates events for inactive participation days. Meeting information is advisory
and does not silently change task effort.

**Current limitation:** the deployed UI reports calendar integration as
`not_configured`; no live OAuth authorization or scheduled calendar synchronization
is enabled in this release. The adapter is an extension point, not a claimed
ready-to-use Microsoft365 connection. Implement and review an operator-approved
provider configuration and token lifecycle before enabling external calls.

## Service desks and directories

Use the [API](api.md) and [directory contract](directory.md). LDAP and API-backed
sources can be selected explicitly when importing employees. There is no built-in
service-desk endpoint, credential, or vendor-specific login form.
