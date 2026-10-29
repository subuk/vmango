# Working on Vmango

## Language

Use English for all code comments, documentation, project and issue titles and
descriptions, issue and review comments, commit messages, and pull requests,
including content maintained in Linear and GitHub. Conversation with the user
may remain in their preferred language.

## Project layout

Vmango is a Go web application for managing KVM virtual machines through
libvirt. Keep business logic separate from HTTP handlers and libvirt calls.

- `main.go`, `bootstrap/`: CLI entry point, application wiring and password tools.
- `compute/`: domain types, service interfaces and virtual machine management.
- `libvirt/`: implementations backed by libvirt and domain XML generation.
- `filesystem/`: filesystem repositories and event hooks.
- `config/`: HCL configuration and defaults; `vmango.dist.conf` is the example.
- `configdrive/`: cloud-init data and ISO generation.
- `web/`: HTTP routing, handlers, sessions, authentication and CSRF protection.
- `templates/`, `static/`: server-rendered UI and bundled browser dependencies.
- `vendor/`: committed Go dependencies; `vendortools/` tracks build tooling.
- `docs/`: Hugo/Docsy documentation, with its own Go and npm dependencies.
- `vmango.spec.in`, `Makefile.RPM.mk`, `.copr/`: RPM packaging and COPR entry point.
- `debian/`, `Makefile.DEB.mk`, `dockerbuild/`: DEB packaging and build containers.

## Build and verification

The application requires Go 1.26 or newer, a C compiler, pkg-config and libvirt
development libraries. Runtime ISO creation requires genisoimage or a compatible
tool. macOS can build and run the application. The example configuration also
includes a local QEMU/libvirt session with a custom emulator and socket-vmnet;
this is distinct from Linux KVM and needs its own integration checks. Use a
Linux libvirt host to verify production KVM behavior.

```sh
make
make test
git diff --check
```

Use `make test` rather than bare `go test ./...` on a clean checkout: it generates
the embedded assets first. The Makefile sets `GOFLAGS=-mod=vendor` and a build
cache under `/tmp`. Format changed Go files with `gofmt`.

Edit `templates/` and `static/`, not `web/assets_generated.go`. The latter and
`bin/` are generated and ignored by Git. If assets were removed or renamed,
remove `web/assets_generated.go` and rebuild to avoid retaining stale content.
The local `vmango.conf` is ignored; do not commit local credentials or machine
settings. Avoid using `make clean` casually: it also removes package artifacts.

Add focused regression tests for behavior changes. Existing tests cover config
merging, device naming, login/CSRF behavior and the VNC page/assets. For UI changes,
check the rendered page in a browser. Tests with fake repositories or a simulated
VNC server do not establish that real hypervisor operations work.

Real VM lifecycle checks live in `acceptance/` behind the `acceptance` build tag.
Run `ACCEPTANCE_IMAGE=/absolute/path/to/cloud.img make acceptance` explicitly;
see `acceptance/README.md` for prerequisites, scope and cleanup. The suite uses
its own random pool/VM and loopback web configuration. Never point cleanup at
existing user machines or reuse their disks. Its logs remain under `result/`.
A macOS/HVF pass does not establish that Linux/KVM or remote hosts work.

Report local Go tests, browser checks and remote package builds separately.
A queued build is not a successful build. Documentation-only changes normally
need a diff check, not a full application test run.

## Dependencies and browser assets

Update `go.mod`, `go.sum` and `vendor/` together. Use `make vendor` to run
`go mod tidy` and `go mod vendor`; do not patch generated vendor files directly.
Check the resulting diff and rebuild. For security updates, run `govulncheck`
when available and distinguish reachable vulnerabilities from module-only
findings. Verify Dependabot after publication rather than dismissing alerts
to make the list empty.

When changing the minimum Go version, also update the README, RPM BuildRequires,
DEB Build-Depends, `debian/rules` compiler path and relevant container images.
Package builds use `GOTOOLCHAIN=local` and vendored modules: install the compiler
in the build environment instead of relying on a toolchain download.

The bundled noVNC is a runtime distribution. Read
`static/noVNC-1.7.0/VMANGO.md` before changing it. Preserve upstream licenses,
module-relative locale/metadata URLs and the mandatory same-origin WebSocket
settings in the VNC template. URL parameters and saved browser settings must
not redirect the console connection. Remote VNC access needs HTTPS.

CSRF protection uses `net/http.CrossOriginProtection` plus the HTTPS-origin
check in `web/csrf.go`. Preserve tests for proxy deployments, cross-site requests
and authentication. Requests without browser origin headers still require
authentication on protected routes.

## Documentation

`docs/` requires Hugo Extended >= 0.163.0, Node.js >= 22 and native Dart Sass
(`sass` on PATH). The npm Sass implementation does not provide Hugo's embedded
protocol.

```sh
cd docs
npm ci
npm run hugo -- --minify
```

After updating Docsy, run `hugo mod npm pack` and `npm install` in `docs/`.
Keep the generated `docs/packages/hugoautogen/` workspace and npm lockfile in
sync. Application and documentation dependencies are separate.

## Packaging and publication

Stable distribution packages are published only from existing stable GitHub
releases by `.github/workflows/publish-packages.yml`. COPR targets RHEL 9 and
RHEL 10 on x86_64; PPA targets Ubuntu Jammy, Noble and Resolute on amd64.

The existing `vmango-devel` COPR/PPA repositories are for manual maintainer
validation via `.github/workflows/test-packages.yml`. No webhook, daily recipe
or push to `master` may publish devel packages. The workflow resolves a trusted
source ref to an exact SHA and uses a unique version below the next release.
Keep shared upload destination validation and the amd64 checks intact.

Publish source changes to GitHub `origin` when publication is requested. Do not
push to the legacy Launchpad `lp` remote or restore `make publish-devel`.
GitHub CI still checks PRs and master without publishing packages. Follow
HOWTO-RELEASE.md for manual tests, release uploads and rerun semantics.

For DEB builds use `./dockerbuild.sh ubuntu-2404 make deb` or `make deb` in a
suitable Linux environment. The compiler package is `golang-1.26-go`, selected
through `/usr/lib/go-1.26/bin`. For RPM use `make rpm` in a suitable RPM environment.
Package builds use vendored modules and `GOTOOLCHAIN=local`.

Check live service configuration and completed build logs when diagnosing
packaging. Do not disable signature verification to work around infrastructure
errors. Report local tests separately from remote builds and installation tests.
Do not bump a release version or create a tag as part of routine maintenance.
