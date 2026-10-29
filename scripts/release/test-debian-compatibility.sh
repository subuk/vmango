#!/usr/bin/env bash
# Run inside a clean Debian amd64 container; use the Ubuntu Jammy package.
set -euo pipefail
: "${PUBLICATION_CHANNEL:?}" "${VERSION:?}" "${PACKAGE_REVISION:?}"
case "$PUBLICATION_CHANNEL" in
  stable) project=vmango ;;
  devel) project=vmango-devel ;;
  *) echo 'Unsupported publication channel' >&2; exit 1 ;;
esac
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl gnupg python3
curl -fsSL 'https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x10538B83BC93D600E0C98A39AF0F396262965789' \
  -o /tmp/vmango-ppa.asc
gpg --show-keys --with-colons /tmp/vmango-ppa.asc | \
  awk -F: '$1 == "fpr" { print $10; exit }' | \
  grep -qx '10538B83BC93D600E0C98A39AF0F396262965789'
gpg --batch --yes --dearmor -o /usr/share/keyrings/vmango-ppa.gpg /tmp/vmango-ppa.asc
printf 'deb [arch=amd64 signed-by=/usr/share/keyrings/vmango-ppa.gpg] https://ppa.launchpadcontent.net/subuk/%s/ubuntu jammy main\n' "$project" \
  > /etc/apt/sources.list.d/vmango.list
apt-get update
apt-get install -y --no-install-recommends "vmango=$VERSION-${PACKAGE_REVISION}ppa1~jammy"
dpkg-query -W -f='${Package} ${Version} ${Architecture}\n' vmango
# Exercise the installed executable with the existing startup/login/libvirt test.
mkdir -p /tmp/vmango-package-smoke/vmango
cp /usr/bin/vmango /tmp/vmango-package-smoke/vmango/
cp /src/vmango.quickstart.conf /tmp/vmango-package-smoke/vmango/
python3 /src/scripts/release/smoke.py /tmp/vmango-package-smoke
