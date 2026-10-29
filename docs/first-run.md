# First administrator setup

Decision accepted by the user on 2026-10-30: remove the installation key from
the first-run flow. The normal application URL opens administrator setup while
the installation has never completed enrollment. Create a username, name and
password, then confirm TOTP and save recovery codes.

Alternatives considered:

| Option | Benefits | Costs and risks |
| --- | --- | --- |
| A: retain installer proof | Only the holder can claim an empty installation. | Extra file/link/key to handle; rejected by the user. |
| B: keyless first administrator (accepted) | Normal URL, no manual setup token. | The first person able to reach an unconfigured installation can become administrator; keep default loopback binding until setup completes. |
| C: create administrator through local CLI | No public first-run enrollment. | Requires terminal access and a separate TOTP handoff; not the requested browser experience. |

The server creates an unpredictable short-lived continuation token after receiving
the administrator details. The browser handles it in memory; it is not a field
users must fill, a URL secret, or an installation key. Confirming TOTP consumes
initial setup atomically. Competing pending attempts cannot create another admin
after the first confirmation. Refreshing before completion starts a new attempt.

Invitation and administrator recovery links still require their own single-use
tokens. Disabling/deleting accounts does not reopen initial setup. Existing
authentication, TOTP, CSRF, origin checks and throttling remain enforced. Legacy
installation-key files are no longer used for first-run authorization.
