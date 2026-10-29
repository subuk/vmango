# Building and publishing releases

## Continuous integration

`.github/workflows/ci.yml` runs on PR creation, new PR commits, reopening a PR,
and every push to `master`. It calls `build.yml`, which builds the same archives
used for releases. Regular CI has read-only repository permissions and never
creates a release or pushes a tag.

- Linux x64: Ubuntu 22.04, system libvirt, `make` and `make test`.
- macOS arm64: macOS 15, Homebrew libvirt and dvdrtools, `make` and `make test`.
- Windows x64: Fedora 44 MinGW cross-compiler and libvirt SDK; the resulting
  binary and compiled Go tests run on a Windows Server 2022 runner. The ZIP
  includes runtime DLLs, `virsh.exe`, upstream notices and package versions.

Go comes from `go.mod`; modules are vendored and `GOTOOLCHAIN=local` prevents
implicit compiler downloads during builds. Build dependencies are installed by
the workflow. Windows cross-compilation uses a Fedora container on the build
runner; **users do not need Docker**.

Every target extracts its archive and runs `scripts/release/smoke.py` against
`vmango.quickstart.conf`, substituting a temporary port, test credentials and
libvirt's `test:///default` URI. It verifies startup, login and node rendering.
Unix runners also install ISO tools for `TestISORoundTrip`; that test skips when
the tools are unavailable, including the default Windows runner.

These checks do not prove real KVM/HVF operations, Windows TLS connectivity or
Windows config-drive provisioning. Before publishing support claims, test VM lifecycle operations on the relevant real hypervisors and
follow the [binary guide](docs/content/en/docs/start.md) with the actual archives.
Record platforms, library versions and results in the release notes. Do not
label a queued workflow or a cross-compile alone as a verified runtime.

## Publish GitHub release assets

The workflow must first exist on the repository's default branch for the
**Run workflow** button to be available. No personal access token is needed;
only the final publication job receives `contents: write`.

1. Prepare the intended release commit. Update `VERSION` in `Makefile` and
   changelogs in `vmango.spec.in` and `debian/changelog`. Commit and push the
   source; normal maintenance does not bump versions or create tags.
2. Open **Actions → Release binaries → Run workflow**. Run the workflow from
   the maintained `master` branch. Enter `version` without `v`, for example
   `0.13.0`, and `source_ref` as a branch, tag or commit SHA containing the build
   scripts and configuration. Prefer a reviewed commit SHA.
3. The prepare job validates the version and resolves the source to one commit.
   All builds check out that SHA. An existing `vVERSION` tag must resolve to
   the same commit. The version input is embedded into the binary.
4. After every build and runtime check passes, the publication job generates
   `SHA256SUMS`, creates a draft release with all assets and then publishes it.
   GitHub creates the `vVERSION` tag at the resolved commit if it does not exist.
   Versions with a suffix, such as `0.13.0-rc.1`, are marked as prereleases.
5. Verify the published release and checksums. Add the real-platform validation
   results to the generated release notes.

Published assets:

```text
vmango-VERSION-linux-x64.tar.gz
vmango-VERSION-windows-x64.zip
vmango-VERSION-macos-arm64.tar.gz
SHA256SUMS
```

Archives contain a top-level directory with the same name as the archive stem.
They include `vmango` or `vmango.exe`, `vmango.quickstart.conf`,
`vmango.dist.conf`, `QUICKSTART.md` and license notices. Linux/macOS runtime
libraries remain system prerequisites. Windows ships the installed Fedora
MinGW DLL set and records the matching source RPM names in
`RUNTIME-PACKAGES.txt`; preserve those notices when redistributing it.

The workflow does not overwrite an existing release or move a tag. If a failed
publication leaves a draft, inspect it and either complete publication manually
or delete the incomplete draft before rerunning. Investigate a tag mismatch
instead of force-pushing. Concurrent runs for the same version are serialized.
The publication job builds no code and uses only artifacts from its own run.

## Reproduce an archive locally

Install Go, a C compiler, pkg-config, libvirt development files and the runtime
ISO tools. With Python 3.12 or newer:

```sh
make VERSION=0.13.0
make test
python3 scripts/release/package.py 0.13.0 linux-x64 bin/vmango
python3 scripts/release/smoke.py dist/vmango-0.13.0-linux-x64.tar.gz
```

Use `macos-arm64` on Apple Silicon. Build on the documented baseline OS;
changing the build host can raise runtime requirements. This is a repeatable
process, not a promise of byte-identical archives: runner images and system
packages receive updates. The Windows commands are in
`scripts/release/windows.sh` and the reusable build workflow.

## Distribution packages and development publication

GitHub binary releases do not publish stable COPR or PPA packages. Build RPMs
with `make rpm` in a supported RPM environment and DEBs with
`./dockerbuild.sh ubuntu-2404 make deb` or `make deb` in a suitable Linux
environment. Check current service configuration before publishing stable
packages; the former CentOS 7 / Ubuntu Bionic release commands are obsolete.

Development publication remains a separate direct-push process:

```sh
make publish-devel
```

It pushes committed local `master` to GitHub `origin`, then to the separately
hosted Launchpad `lp` repository without force. Check both push results. COPR
watches GitHub `master`; Launchpad's `master-daily` recipe reads `lp` and builds
daily or on request. Neither process publishes a stable release automatically.
