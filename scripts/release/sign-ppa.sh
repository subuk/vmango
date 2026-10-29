#!/usr/bin/env bash
set -euo pipefail
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
