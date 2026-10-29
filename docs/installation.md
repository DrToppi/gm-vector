# Installation

## Requirements

Use Linux amd64, or Linux inside WSL2 on Windows with Docker Desktop integration.
You need Docker Engine with the Compose v2 plugin, Python 3.10+, OpenSSL, and enough disk
space for images and persistent database volumes. Source builds additionally need
Go 1.27+, Node.js 22.12+ and npm. Dependency versions are pinned in lock files.
The installer never installs Docker or changes your host trust store.

## Build from source

From the repository root:

```sh
python3 production/scripts/build.py
# Run all three builds against the same production build context.
docker build -f production/Dockerfile -t gm-vector:local production
docker build -f production/Dockerfile.db -t gm-vector-db:18.6 production
docker build -f production/Dockerfile.proxy -t gm-vector-proxy:local production
python3 production/deploy/install.py init --state "$HOME/.local/share/gm-vector"
python3 production/deploy/install.py prepare --state "$HOME/.local/share/gm-vector"
```

The installer detects a rootless Docker socket unless DOCKER_HOST/DOCKER_CONTEXT
is already set. Use the same Docker context for builds and installation.
`init` requires a new directory and refuses to overwrite existing keys.
Use `--port 19443 --subnet 172.30.94.0/29` with `init` if defaults conflict.

`prepare` creates separate database runtime and migration roles, applies schema
migrations, seeds a calendar, and starts API, worker, and reverse proxy. It prints
the web address and the path to a private `setup-url.txt`. It never creates a
project or imports working data. Open the private setup link locally, create your
administrator username/name/password, enroll TOTP, then store recovery codes
securely offline. Setup is closed after the first administrator is created.

## HTTPS and network access

Default: https://localhost:9443, bound to loopback, with a locally generated self-signed certificate.
The installer validates readiness against that certificate. Browser trust must be configured
by the operator, or use your own trusted certificate. See [HTTPS](https.md) for
PEM certificates, corporate CAs, external public-domain ACME provisioning, and explicit network binding.
Do not disable certificate, Host, Origin or CSRF validation to bypass errors.

## Operations

Use the same `--state` for every command:

```sh
python3 production/deploy/install.py status --state "$HOME/.local/share/gm-vector"
python3 production/deploy/install.py stop --state "$HOME/.local/share/gm-vector"
python3 production/deploy/install.py start --state "$HOME/.local/share/gm-vector"
```

Back up database and configuration together. Keep the state directory outside the
repository, and protect encryption keys, bootstrap/recovery links and SMTP/LDAP
configuration. Do not delete named volumes during updates. Rebuild/pull approved
images, take a verified backup, then run `prepare` to apply migrations and recreate
services. There is a maintenance window; rollback requires restoring the matched
backup, not running an older binary against a newer schema.

See [recovery](recovery.md) for administrator and backup recovery.

## Offline distribution

Build locally, then create a new directory outside the source repository:

```sh
python3 production/deploy/package.py --app-image gm-vector:local \
  --db-image gm-vector-db:18.6 --proxy-image gm-vector-proxy:local \
  --out /absolute/new/gm-vector-package
```

The directory contains images, installer, notices and checksums. Archive that
folder and transfer it. On the target host, run `python3 install.py init --state
/absolute/private/state`, then `python3 install.py prepare --state
/absolute/private/state`. Verify provenance through a trusted release channel;
checksums bundled with an archive detect corruption but do not authenticate its
publisher. No existing projects, credentials, or populated configuration belong
in a distributable archive.

## Optional services

[LDAP and directory API](directory.md) are opt-in. SMTP is configured by the
operator through a protected local file; see [integrations](integrations.md).
Calendar provider interfaces remain available; the Microsoft adapter is disabled
until deployment-specific authorization/configuration is implemented. No third-party
API is contacted merely by installing the application.
