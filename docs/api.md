# Integration API

GM Vector exposes versioned JSON endpoints under `/api/v1`. The complete contract
is [OpenAPI](contracts/openapi.json), also available to an authenticated user at
`GET /api/v1/openapi.json`. Generate it with
`python3 production/scripts/openapi.py`; route discovery fails if a mutation lacks
a request contract. External adapters run separately from the planner.

## Authentication and boundaries

The browser uses a Secure, HttpOnly, SameSite=Strict session cookie. Browser
mutations require the exact deployment Origin and X-CSRF-Token. Service adapters
use `Authorization: Bearer TOKEN`, with explicit scopes, project IDs and expiry
(maximum 90 days). Tokens are opaque random secrets, shown at creation only. Never
embed one in a URL, frontend build, repository or diagnostic report.

An administrator issues tokens in the application. Available scopes:

| Scope | Use |
| --- | --- |
| `project:read` | Read allowed projects and task/time data |
| `project:write` | Modify allowed projects using revision control |
| `export` | Export allowed projects |
| `directory:read` | Search authoritative directory snapshots |
| `directory:write` | Synchronize an API directory namespace |

Scopes do not grant administrator routes or access to other projects. Directory
tokens must belong to an active administrator; they cannot create accounts,
assign project roles or send invitations. A directory-write token can update
any API source namespace, so only give it to a trusted directory administrator.
The configured LDAP namespace is reserved and cannot be overwritten through API.
Revoking a token or disabling its user prevents subsequent authenticated requests.
An already-authorized in-flight request may finish.

## Read and update a project

Read `GET /projects/{id}` to obtain `{project, role}`. Keep the returned revision.
Modify the project payload, then optionally call `POST /projects/{id}/preview`
with `{ "project": ... }` to inspect calculated dates and warnings. Commit with
`PUT /projects/{id}` and the same shape. On 409, refetch and reconcile; do not
blindly replay stale writes. The server validates hierarchy/dependency cycles,
participation and capacity before applying changes.

`external_reference` is a free-form project-level reference (up to 500 UTF-8 bytes)
for your own system. It does not trigger outbound calls. Your adapter owns the
mapping, authentication and status synchronization with its external system.

```python
# Minimal read-only adapter, standard library only.
# Supply environment variables locally, never commit their values.
import json, os, urllib.request
base = os.environ['GM_VECTOR_ORIGIN'].rstrip('/')
request = urllib.request.Request(
    base + '/api/v1/projects',
    headers={'Authorization': 'Bearer ' + os.environ['GM_VECTOR_TOKEN']})
with urllib.request.urlopen(request, timeout=15) as response:
    projects = json.load(response)
```

Use HTTPS and the host trust store. Never set certificate verification to false.
Apply bounded timeouts/backoff; 429 is a rate limit, 422 a validation failure,
403 permission denial,404 absent or concealed, and503 an unavailable integration.
Responses include a request ID for correlating sanitized operator diagnostics.
Do not log request bodies or tokens.

## Directory import

See [directory](directory.md) for schemas, stable identities, LDAP and lifecycle.
`POST /directory/sync` accepts a provider namespace and at most 1000 entries.
The response is `{processed, disabled, missing}`: processed input records, linked
accounts disabled in this call (not a newly-disabled transition counter), and stored
identities absent from this batch. The batch is atomic. Explicit inactive records revoke bound access; records
missing from the batch are reported for review, not deleted or disabled.

Search `GET /directory/people?q=...&source=ldap|api&provider=...`; source and
provider are optional and combine as filters. The status endpoint returns
`{ldap:{configured,enabled,provider}, sources:[string],
source_details:[{provider,kind}]}`. Status and LDAP sync require an administrator
session; a service token cannot call them. Project scopes require explicit project
IDs; directory-only tokens may use an empty project list.

## Idempotency and limits

Task comments and employee invitations use caller-generated UUIDs; an identical
retry returns the existing result. Never reuse a UUID for different content.
Project writes use optimistic revisions. Maximum request JSON: 5 MiB; projects:
300 tasks and 1500 links. These are validation limits, not commercial entitlements.
Dates use ISO8601; effort/capacity values use integer minutes. Server-calculated
summary, actual and blocked fields must not be treated as authoritative client input.

## Compatibility

The first public release provides REST v1 and a fresh database baseline. Project
JSON must use `external_reference`; older private export formats require separate
offline conversion before import. Do not attach this release to an existing private
database or rewrite its migration journal. Imported projects intentionally clear
external bindings to prevent accidental cross-system updates. An adapter should
explicitly set its own reference after import.
