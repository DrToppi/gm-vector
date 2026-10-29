# gmVector installer

Linux amd64 or Windows with WSL2 and Docker Desktop integration. Install Docker
Engine/Desktop, the Compose plugin, Python 3.10+, and OpenSSL first. The installer does not
install system packages or change the host trust store.

From an unpacked offline archive:

```sh
python3 install.py init --state "$HOME/.local/share/gm-vector"
python3 install.py prepare --state "$HOME/.local/share/gm-vector"
```

From a source checkout, first build the three images as described in
`docs/installation.md`, then use `production/deploy/install.py` in place of
`install.py`. The installer uses local image IDs in source mode; the offline
package verifies its file checksums and loads bundled images.

The installer prints the HTTPS address and the location of a private setup link.
Open that link locally, choose administrator credentials, enroll TOTP, and save
the recovery codes offline. Do not share the setup link. Default access is
loopback-only with a locally generated localhost certificate; see `docs/https.md` for externally trusted TLS.

Use `status`, `stop`, and `start` with the same `--state` argument. Use
`recover-admin --username USERNAME --state PATH` for host-authorized administrator
recovery; it revokes sessions/tokens and writes a private, single-use 15-minute link.

Keep the state directory and Docker volumes: they contain the database, encryption
keys, configuration and encrypted reports. They are never part of a distribution.
Checksums detect corruption; verify the release provenance separately when
receiving an archive from someone else.

The installer generates a unique 365-day localhost certificate in `STATE/tls`.
`renew-local-tls --state PATH` generates its replacement; `apply-https --state PATH`
validates and activates it. Local browser trust may need updating after renewal.
External certificates are issued/renewed with your CA or an external ACME client,
then copied as regular `fullchain.pem` and private `privkey.pem` files into the
configured directory and activated with `apply-https`. No ACME client or CA private
key runs inside the web proxy. See `docs/https.md` for the full procedure.
