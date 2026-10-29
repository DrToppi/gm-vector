# Security

Do not publish credentials, private keys, customer data, or sensitive diagnostic
reports in public issues. Report suspected vulnerabilities through the repository's
private vulnerability-reporting channel when available. If unavailable, open a
minimal issue requesting a private contact, without exploit details or private data.

Include the affected version, the relevant trust boundary, and a minimal synthetic
reproduction. Do not test other people's installations without permission.

Operators are responsible for HTTPS, trusted proxy configuration, restricted
network access, backups, and protecting deployment secrets. No vendor needs access
to your private signing keys, directory credentials, or database.

Bug reports are encrypted before storage. The operator controls the public support
key and retains the private key separately. User-written descriptions can contain
sensitive information: review them before submission. Reports are not automatically
forwarded to an external support service.
