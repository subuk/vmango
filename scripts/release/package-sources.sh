#!/usr/bin/env bash
set -euo pipefail
: "${VERSION:?}" "${PACKAGE_REVISION:?}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(~test\.[1-9][0-9]*\.g[0-9a-f]{40})?$ ]]
[[ "$PACKAGE_REVISION" =~ ^[1-9][0-9]*$ ]]
python3 scripts/ui-assets.py verify
mkdir -p dist/packages/rpm dist/packages/ppa
# Archive the exact checkout, including vendored dependencies and test fixtures.
# Persist the requested version for builders without Git metadata.
metadata=$(mktemp -d)
trap 'rm -rf "$metadata"' EXIT
mkdir -p "$metadata/vmango-$VERSION"
printf '%s\n' "$VERSION" > "$metadata/vmango-$VERSION/VERSION"
git archive --format=tar --prefix="vmango-$VERSION/" HEAD > "$metadata/source.tar"
cp -R ui/dist "$metadata/vmango-$VERSION/ui-dist"
mkdir -p "$metadata/vmango-$VERSION/ui"
mv "$metadata/vmango-$VERSION/ui-dist" "$metadata/vmango-$VERSION/ui/dist"
tar -rf "$metadata/source.tar" -C "$metadata" "vmango-$VERSION/VERSION" "vmango-$VERSION/ui/dist"
# Verify the extracted archive rather than trusting the working copy.
mkdir "$metadata/verify"
tar -xf "$metadata/source.tar" -C "$metadata/verify"
python3 "$metadata/verify/vmango-$VERSION/scripts/ui-assets.py" verify
gzip -n < "$metadata/source.tar" > "dist/packages/vmango-$VERSION.tar.gz"
mkdir -p dist/packages/rpmbuild/{SOURCES,SPECS,SRPMS}
cp "dist/packages/vmango-$VERSION.tar.gz" dist/packages/rpmbuild/SOURCES/
sed -e "s/@@_VERSION_@@/$VERSION/g" -e "s/@@_RELEASE_@@/$PACKAGE_REVISION/g" vmango.spec.in > dist/packages/rpmbuild/SPECS/vmango.spec
docker run --rm -v "$PWD/dist/packages:/packages" fedora:44 bash -euc '
  dnf install -y rpm-build
  rpmbuild -bs --define "dist .release" --define "_topdir /packages/rpmbuild" /packages/rpmbuild/SPECS/vmango.spec
  cp /packages/rpmbuild/SRPMS/*.src.rpm /packages/rpm/
'
for series in jammy noble resolute; do
  work="$PWD/dist/packages/ppa/$series"
  mkdir -p "$work"
  cp "dist/packages/vmango-$VERSION.tar.gz" "$work/vmango_$VERSION.orig.tar.gz"
  tar -xzf "$work/vmango_$VERSION.orig.tar.gz" -C "$work"
  (
    cd "$work/vmango-$VERSION"
    DEBFULLNAME='Vmango release CI' DEBEMAIL='admin@vmango.org' \
      dch --force-distribution --distribution "$series" \
      --newversion "$VERSION-${PACKAGE_REVISION}ppa1~$series" \
      "Package $VERSION from $(git -C "$OLDPWD" rev-parse HEAD)."
    # Source-only build: the Launchpad builders install compiler dependencies.
    dpkg-buildpackage -S -sa -d -nc -us -uc
  )
  rm -rf "${work:?}/vmango-$VERSION"
done
rm -rf dist/packages/rpmbuild
