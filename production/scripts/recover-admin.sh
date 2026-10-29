#!/bin/sh
# Host-controlled recovery; caller must already have access to this deployment.
set -eu
if [ "$#" -ne 3 ]; then
  echo 'Usage: recover-admin.sh /path/to/planner /private/config.json ADMIN_USERNAME' >&2
  exit 2
fi
binary=$1
config=$2
username=$3
umask 077
output_dir=$(mktemp -d "${TMPDIR:-/tmp}/gm-vector-recovery.XXXXXX")
output="$output_dir/recovery-url.txt"
if ! "$binary" recover-user --config "$config" --username "$username" --file "$output"; then
  rmdir "$output_dir" 2>/dev/null || true
  exit 1
fi
printf 'Recovery link saved to %s (valid for 15 minutes). Open locally; delete the file after use.\n' "$output"
