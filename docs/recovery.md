# Recovery

## Lost TOTP device

First try one of the offline, one-use recovery codes issued during enrollment.
A recovery code replaces the current TOTP code during login. Keep these codes
separate from the device and password.

## Host-authorized local administrator recovery

A deployment operator with local Docker access can issue a one-use recovery link:

```sh
python3 production/deploy/install.py recover-admin \
  --state "$HOME/.local/share/gm-vector" --username ADMIN_USERNAME
```

For an unpacked offline package, use `install.py` in place of the source path.
This works only for an existing local administrator. It revokes previous sessions,
API tokens, and pending enrollment, then writes a private link valid for 15 minutes.
Open the link locally, set a new password and TOTP, and save new recovery codes.
Delete the local link file after use. Neither the API response nor installer output
prints the secret. The installer explicitly requests the local CLI transport
`recover-user --file /tmp/PRIVATE_FILE --output-stdout`, captures its output without
logging it, validates the exact HTTPS origin and token format, and writes an
exclusive 0600 host file. The database reset commits before transfer; the temporary
container file is removed. Do not run that explicit transport directly in a logged
terminal. Without the flag, the CLI only creates its protected file. Directory-backed users cannot use this to gain a local-password
fallback. A directory outage does not prevent local administrator login.

For a non-container deployment, use:

```sh
sh production/scripts/recover-admin.sh /absolute/planner \
  /private/config.json ADMIN_USERNAME
```

This is a privileged local operation, not a password-reset endpoint available to
anonymous visitors. Protect access to Docker and configuration accordingly.

## Database recovery

A usable backup contains database state plus the matching encryption key and
runtime configuration. A database dump alone cannot decrypt protected TOTP or
queued mail data. Do not mix secrets from another installation. Store backups
encrypted, keep backup keys separately, and regularly test restores in an isolated
network. The backup utility and its CLI are in `production/scripts/backup.py`.
Run `python3 production/scripts/backup.py --help` for command options. Restore
verification must never point at a real workspace or enable outbound email.
