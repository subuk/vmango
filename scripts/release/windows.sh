#!/usr/bin/env bash
set -euo pipefail
# Fedora ships a complete, mutually compatible MinGW libvirt SDK.
dnf install -y mingw64-gcc mingw64-libvirt mingw64-pkg-config make python3
export PATH="/opt/go/bin:$PATH"
export GOCACHE=/tmp/vmango-go-cache
make web/assets_generated.go
export GOOS=windows GOARCH=amd64 CGO_ENABLED=1
export CC=x86_64-w64-mingw32-gcc PKG_CONFIG=x86_64-w64-mingw32-pkg-config
go build -trimpath -ldflags="-X subuk/vmango/web.AppVersion=$VERSION" -o bin/vmango.exe .
mkdir -p result/windows-tests
go test -c -o "$PWD/result/windows-tests/" ./...
python3 scripts/release/package.py "$VERSION" windows-x64 bin/vmango.exe \
  /usr/x86_64-w64-mingw32/sys-root/mingw/bin
