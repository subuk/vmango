#!/bin/sh
set -eu

# Source archives carry the version selected by the release workflow.
if [ -f VERSION ]; then
    cat VERSION
    exit 0
fi

# Only stable release tags participate in package version discovery.
if [ "${1:-}" = --base ]; then
    if version=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude '*-*' 2>/dev/null); then
        printf '%s\n' "${version#v}"
    else
        printf '%s\n' 0.0.0
    fi
elif version=$(git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null); then
    printf '%s\n' "${version#v}" | sed 's/-\([0-9][0-9]*\)-g/.\1.g/'
elif revision=$(git rev-parse --short HEAD 2>/dev/null); then
    printf '0.0.0.g%s\n' "$revision"
else
    printf '%s\n' 0.0.0-unknown
fi
