#!/usr/bin/env bash
set -euo pipefail
case "${2:-}" in
  '') upload_args=() ;;
  --simulate) upload_args=(--simulate) ;;
  *) echo 'Expected optional --simulate' >&2; exit 1 ;;
esac
: "${PPA_GPG_PRIVATE_KEY:?}" "${PPA_GPG_PASSPHRASE:?}" "${PPA_GPG_FINGERPRINT:?}"
export GNUPGHOME
GNUPGHOME=$(mktemp -d)
trap 'gpgconf --kill gpg-agent || true; rm -rf "$GNUPGHOME"' EXIT
chmod 700 "$GNUPGHOME"
printf '%s' "$PPA_GPG_PRIVATE_KEY" | gpg --batch --import
printf '%s' "$PPA_GPG_PASSPHRASE" > "$GNUPGHOME/passphrase"
chmod 600 "$GNUPGHOME/passphrase"
cat > "$GNUPGHOME/sign" <<'SIGN'
#!/bin/sh
exec gpg --batch --pinentry-mode loopback --passphrase-file "$GNUPGHOME/passphrase" "$@"
SIGN
chmod 700 "$GNUPGHOME/sign"
debsign -p"$GNUPGHOME/sign" -k"$PPA_GPG_FINGERPRINT" "$1"
# dput verifies signatures using this keyring before uploading.
dput "${upload_args[@]}" ppa:subuk/vmango "$1"
