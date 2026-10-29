# HTTPS certificates

The default deployment keeps Caddy's local `tls internal` configuration. Operators
can opt into their own PEM certificate or automatic ACME issuance through a
Caddyfile and Compose overlays. No production certificates were changed by this
implementation. Private keys must never enter Git, images, archives or Bug Reports.

## Bring your own certificate

Prepare `fullchain.pem` (leaf first, followed by intermediates) and the matching
unencrypted `privkey.pem` outside the repository. The certificate SAN must cover
the configured hostname/IP. The directory is mounted read-only into **proxy only**;
API and worker never receive it.

Caddy runs as container UID/GID `1000:1000`. Give this identity read access using
ownership/group permissions, for example directory0750 and key0640. Never make
the key world-readable. Rootless Docker translates container IDs: container
UID1000 is not necessarily host UID1000. Resolve the mapping rather than using
`chmod 777`. Validation below checks whether the proxy can load the pair.

For the packaged installation:

1. Back up `state/Caddyfile`, `state/config/config.json` and any existing
   `state/https.json` outside Git. Do not rerun installation initialization.
2. Copy `tls/Caddyfile.files` to `state/Caddyfile`. The proxy needs read access
   to the Caddyfile as well.
3. Create `state/https.json`:

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

`ca_file` is the public trust anchor for a private CA, **not its private key**.
Omit it for a publicly trusted certificate; system trust is used. The default
bind address stays loopback. Set a specific interface or `0.0.0.0`/`::` explicitly
for network access. HTTPS port is taken from origin, default443. Rootless Docker
may need host configuration to publish ports below1024; with your own certificate
you can instead use `https://planner.example.com:9443` and HTTP port9080.

4. Change only `origin` in `state/config/config.json` to the exact same string.
   Do not weaken Host/Origin/CSRF checks to work around mismatches.
5. Validate and apply, replacing the state path:

```sh
python3 install.py check-https --state /absolute/state
python3 install.py apply-https --state /absolute/state
```

Validation runs `caddy validate` in a temporary proxy container without publishing
ports. Invalid configuration prevents recreation of the running proxy. Apply
repeats validation, copies the existing source configuration into the secrets
volume, recreates API/worker/proxy, and checks `/readyz` using verified TLS.
There is a brief restart: `admin off` is retained, so `caddy reload` is unavailable.
The readiness probe connects to the local published port while using the public
hostname for SNI/certificate verification and the exact public Host header.
It neither follows redirects nor uses HTTP proxy environment variables.

If application fails after validation, restore the saved configuration and prior
certificate pair, then run apply again. To return to the previous internal TLS,
remove only the new `https.json`, restore the old Caddyfile and backend origin,
then apply. Do not delete Docker volumes or regenerate database keys.

## Automatic ACME for a public domain

Use `tls/Caddyfile.acme` instead, with:

```json
{
  "mode": "acme",
  "origin": "https://planner.example.com",
  "bind_address": "0.0.0.0",
  "http_port": 80
}
```

Set the same backend origin. DNS A/AAAA records must lead to this installation
and external ports80/443 must reach Caddy. The overlay forwards them to container
ports9080/9443, allowing an unprivileged proxy. Keep persistent `caddy_data` and
`caddy_config`: they hold managed certificate state.

Caddy obtains and renews managed certificates. Successful validation does not
prove that an external CA can issue one: DNS/network reachability still matters.
Tests do not perform live issuance. This profile requires external80/443;
nonstandard ACME ports are rejected. Wildcard/DNS challenge needs an appropriate
DNS provider module, whose presence is not promised by this configuration.

## Standard production Compose

The same templates and overlays work with `production/compose.yaml`. Set these
non-secret values in the deployment env file: absolute `GMA_CADDYFILE`,
`GMA_HTTPS_HOST` (certificate hostname without port), `GMA_PUBLIC_ORIGIN`,
`GMA_BIND_ADDRESS`, `GMA_HTTPS_PORT`, `GMA_HTTP_PORT`, and `GMA_TLS_DIR` for files
mode. IPv6 host/bind values in Compose/Caddy require square brackets.
Compose2.24.4+ is required for `!override`, replacing the default ports rather
than leaving an unintended additional listener.

```sh
docker compose --env-file /private/compose.env \
  -f production/compose.yaml -f production/compose.https.yaml \
  -f production/compose.https.files.yaml \
  run --rm --no-deps proxy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
```

Omit `compose.https.files.yaml` for ACME. Always use the same daemon, project,
env and overlay list for deployment commands. Update the backend origin in its
source config, run `init-secrets`, then
`up -d --no-deps --force-recreate api worker proxy` with the same Compose options.
The old `production/scripts/service.sh` does not automatically add these overlays;
use explicit Compose commands. Verify the new HTTPS URL with a trusted CA;
`curl -k` is not an acceptance check.

## Renewal and rollback

For operator-managed certificates, stage a complete new pair in another private
directory, update cert_dir and validate before applying. Keep the old pair for
the rollback period. Caddy does not renew manually supplied certificates.
Do not partially overwrite a key file currently read by a service. ACME renewal
is managed by Caddy; do not discard its persistent storage during upgrades.

## Verification

- `python3 -m unittest discover -s production/demo -p 'test_*.py'` covers default
  and custom origins, CA/SNI, permissions, invalid settings and apply ordering.
- `python3 production/scripts/https-check.py` starts a real unprivileged Caddy with
  a synthetic CA: valid TLS, mismatched key, wrong hostname/untrusted CA, trusted
  proxy header replacement. ACME is adapted only; no external issuance occurs.
- Validate merged Compose profiles: one Caddyfile mount, replacement ports,
  certificate mount read-only and absent from every service except proxy.

Primary sources checked2026-10-29:
[Caddy TLS](https://caddyserver.com/docs/caddyfile/directives/tls),
[Automatic HTTPS](https://caddyserver.com/docs/automatic-https),
[CLI validation](https://caddyserver.com/docs/command-line#caddy-validate).
