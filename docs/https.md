# HTTPS certificates

The bundled nginx proxy terminates TLS; certificate issuance and renewal are
separate operator responsibilities. Default installation generates a localhost
self-signed certificate with OpenSSL. nginx does not contact an ACME service or
renew certificates automatically. Private keys must never enter Git, images,
distribution archives or Bug Reports.

## Local installation

The installer creates `state/tls/fullchain.pem` and `state/tls/privkey.pem` during
`init`. The private key stays protected on the host. Certificate material is
staged into a dedicated proxy certificate volume for container UID/GID 1000:1000;
API and worker do not receive that volume. The proxy mounts it read-only.

Readiness verifies TLS against the local certificate. Browser trust is a separate,
explicit operator action; the installer does not modify the host trust store.
Do not disable TLS validation to bypass a trust error. Repeated `prepare` keeps
the existing certificate and installation keys. The generated certificate lasts
365 days and covers localhost, 127.0.0.1 and ::1. Renew it explicitly before expiry:

```sh
python3 production/deploy/install.py renew-local-tls --state /absolute/state
python3 production/deploy/install.py apply-https --state /absolute/state
```

Renewal replaces the local pair; `apply-https` activates it. Retain a protected
backup for rollback, and update browser trust after replacing a self-signed
certificate. Use this operation only for the local certificate, not externally
issued certificates.

## Operator-managed certificates

Prepare `fullchain.pem` (server certificate first, then intermediate certificates)
and matching unencrypted `privkey.pem` in a protected directory outside the repo.
The certificate SAN must match the public hostname/IP. The host installer must be
able to read regular files; symlink certificate paths are rejected. Copy resolved
Certbot files into a private staging directory. Do not make the private key world-readable. nginx validates
that the certificate and key match. [nginx HTTPS configuration](https://nginx.org/en/docs/http/configuring_https_servers.html).

Back up `state/nginx.conf`, `state/config/config.json` and any existing
`state/https.json` before changing HTTPS. Create `state/https.json`:

```json
{
  "mode": "files",
  "origin": "https://planner.example.com",
  "bind_address": "127.0.0.1",
  "http_port": 80,
  "cert_dir": "/absolute/private/tls",
  "ca_file": "/absolute/private/company-root-ca.pem"
}
```

`ca_file` is a public trust anchor, never a CA private key. Omit it for certificates
trusted by the system. Set `origin` in `state/config/config.json` to exactly the
same URL. Keep Host, Origin and CSRF checks enabled.

The default network bind remains loopback. Use a specific interface or `0.0.0.0`
only when network access is intended. The HTTPS port comes from origin, default
443. Rootless Docker may require host configuration for ports below 1024; a
custom origin port 9443 and HTTP port 9080 are an alternative.

From the source repository root:

```sh
python3 production/deploy/install.py check-https --state /absolute/state
python3 production/deploy/install.py apply-https --state /absolute/state
```

In a distribution archive, use `install.py` instead. Validation precedes service
recreation. The one-shot syntax validator has no network and read-only mounts;
it temporarily uses root with only DAC_OVERRIDE to read host-owned 0600 PEM
under rootful Docker. The running proxy remains UID1000 with all capabilities
dropped. Apply refreshes the staged configuration/certificates and checks
readiness with verified TLS. Allow a maintenance window. A successful syntax
check is not proof of public DNS reachability or external certificate issuance.

## Public certificates and automated renewal

Use your organization's certificate service or an external ACME client such as
Certbot. With a suitable DNS provider plugin, DNS-01 issuance does not require
opening an HTTP challenge endpoint on Planner. Keep DNS credentials in the
certificate service's protected configuration, outside Planner and its images.
The manual DNS method does not renew unattended without an authentication hook.
[Certbot authenticators](https://eff-certbot.readthedocs.io/en/stable/using.html#dns-plugins).

The operator must install and schedule the chosen certificate client. Configure a
successful-renewal deploy hook that:

1. Copies the renewed full chain and key into a new private staging directory.
2. Updates `cert_dir` to that directory while retaining the previous pair for rollback.
3. Runs `check-https`, followed by `apply-https` using the installation owner's
   Docker context, state path and permissions.
4. Reports failures without printing private keys or application secrets.

Run the hook only for this application's certificate. A root-owned Certbot job
must not accidentally target a different root-owned Docker daemon; explicitly run
the deployment step as the installation owner. nginx cannot see the host's
renewed files until the installer stages them into its volume. Test renewal and
the deployment hook before relying on the schedule. Certbot supports deploy hooks
for successful renewals. [Certbot renewal](https://eff-certbot.readthedocs.io/en/stable/using.html#renewing-certificates).

The old `mode: acme` configuration is unsupported. Use `mode: files` after obtaining
a certificate externally. No live certificate issuance is performed by tests.

## Rollback and checks

If application fails, restore the saved configuration and previous certificate
pair, then apply again. Do not delete database volumes or regenerate application
encryption/bootstrap keys. Keep private backups outside distributable archives.

Installer unit tests live in `production/deploy`; HTTPS integration checks use
`production/scripts/https-check.py` with the locally built nginx proxy image.
The Go proxy integration test also needs native `nginx` on PATH or the
`GMA_NGINX_BIN` environment variable set to its executable. CI installs nginx in
its disposable runner. The disposable installation check is `production/scripts/install-check.py`.
These checks cover synthetic TLS and installation behavior, not your public DNS,
certificate authority account or renewal scheduler.
