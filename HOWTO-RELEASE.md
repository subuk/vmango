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

CI also calls `docker.yml` to build and test the Linux amd64 runtime image.
PRs and pushes to `master` never log in to Docker Hub or publish images.

Go comes from `go.mod`; modules are vendored and `GOTOOLCHAIN=local` prevents
implicit compiler downloads during builds. Build dependencies are installed by
the workflow. Windows cross-compilation uses a Fedora container on the build
runner; **users do not need Docker**.

Every target extracts its archive and runs `scripts/release/smoke.py` against
`vmango.quickstart.conf`, substituting a temporary port, test credentials and
libvirt's `test:///default` URI. It verifies startup, JWT login, the node API and embedded assets from outside
the checkout. The browser SPA must also be checked in a real browser.
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

1. Prepare, commit and push the intended source changes. The version is chosen
   in the release form; no manual version update in the Makefile or package
   changelogs is required. Historical changelog entries remain unchanged.
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

## Docker Hub publication

After publishing GitHub assets, `release.yml` calls `publish-docker.yml` to
publish `subuk/vmango:VERSION` to [Docker Hub](https://hub.docker.com/r/subuk/vmango).
Configure the repository Actions secret `DOCKERHUB_TOKEN` with a Docker Hub
access token for `subuk` that can write to `subuk/vmango`. This secret is available
only to the upload job; source compilation and runtime tests have no publication
credentials. Use the maintained `master` workflow and trusted source commits.

The workflow verifies an existing published release and resolves its tag to an
exact SHA. `docker.yml` builds that source using the Node version in `ui/.nvmrc`
and Go 1.26 with vendored modules and `GOTOOLCHAIN=local`. Updating either toolchain
requires updating the matching Dockerfile image. The frontend is built from
source and its manifest verified before Go compilation. The build runs Go tests;
the final Debian Bookworm image includes libvirt, SSH, CA certificates and ISO
tools, runs as UID/GID `10001:10001` and stores state in `/var/lib/vmango`.

Before export, `scripts/release/docker-smoke.py` checks startup, JWT login, the
embedded UI and version, a `test:///default` libvirt host, and signing key
persistence across restart. It uses its own temporary container and named
volume, never host VM storage. This does not establish real KVM operation or
SSH/TLS access to production hypervisors.
The smoke test configures the private HTTP backend for a trusted HTTPS reverse
proxy, since Docker-forwarded requests do not arrive from loopback. It does not
test TLS termination; container deployments must configure their HTTPS proxy.

The upload job loads the tested image artifact, verifies its source/version
labels, and pushes it without rebuilding. Only the version currently marked as
GitHub's latest stable release updates `subuk/vmango:latest`. Prereleases and
reruns for older versions publish only their version tag. Publication jobs are
serialized to avoid competing `latest` updates.

Run **Actions → Publish Docker image → Run workflow** with an existing release
version, without `v`, to retry Docker publication independently. The source tag
must already contain the Docker build workflow and smoke script. A missing token,
build/test failure or push failure fails the workflow; a published GitHub release
is not rolled back. Inspect the completed job and Docker Hub digest after upload.
GitHub assets, Docker Hub and distribution packages are separate destinations.

To build and verify locally from the repository root:

```sh
docker build --build-arg VERSION=0.13.0 \
  --build-arg REVISION="$(git rev-parse HEAD)" -t vmango:tested .
python3 scripts/release/docker-smoke.py vmango:tested 0.13.0
```

See the [Docker installation instructions](README.md#docker-linux-amd64) for
configuration, state persistence and libvirt connectivity. The build context
allows only application inputs and excludes local configuration, Git metadata
and generated assets.

## Automatic build versions

`make print-version` shows the version used by a local build. An exact `vX.Y.Z`
tag produces `X.Y.Z`; subsequent commits produce `X.Y.Z.N.gSHA`. Prerelease tags
are supported too. A checkout without reachable release tags uses `0.0.0.gSHA`.
Fetch tags and history in shallow checkouts to recover the release-based version.

`make VERSION=X.Y.Z` (or the `VERSION` environment variable) overrides discovery,
as the release workflow must build before publishing its tag. Generated source
archives include a `VERSION` file, which takes precedence over Git discovery.
Sources without either metadata source report `0.0.0-unknown`. CI source-package
checks use the most recent stable tag's base version, or `0.0.0` before the first
tag; these packages are not published.

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

## Distribution packages

GitHub binary releases can optionally publish stable COPR and PPA packages using
the jobs described below. To build packages locally, build RPMs
with `make rpm` in a supported RPM environment and DEBs with
`./dockerbuild.sh ubuntu-2404 make deb` or `make deb` in a suitable Linux
environment. Check current service configuration before publishing stable
packages; the former CentOS 7 / Ubuntu Bionic release commands are obsolete.

## Manual package validation

The existing `subuk/vmango-devel` COPR project and `ppa:subuk/vmango-devel` PPA
are reserved for maintainer testing. They have no automatic builds from
`master`: the COPR webhook is disabled and Launchpad's `master-daily` recipe
has daily builds disabled. The hosted Launchpad Git repository is no longer
part of package publication. Do not restore `make publish-devel` or push to
Launchpad to trigger package builds.

Run **Test distribution packages** in GitHub Actions:

1. Select a trusted source branch, tag or commit and the next stable version
   without `v`, for example `0.13.0`.
2. Select COPR, PPA, or both. COPR builds RHEL and EPEL 8/9/10 on x86_64; PPA builds
   Jammy/Noble/Resolute on amd64 only.
3. The workflow resolves the source to a commit SHA and generates
   `0.13.0~test.RUN_ID.gFULL_SHA`. This sorts below `0.13.0` in RPM and Debian
   version ordering. A new run gets a new version; rerunning failed jobs resumes
   monitoring the same upload without reusing a version for different sources.
4. Wait for completed remote builds and PPA binary publication. Inspect the
   service logs, then test package installation and startup in clean target
   environments. A successful build does not establish real KVM operation.
5. Fix failures in the branch and start a new run. Release the tested commit
   under its final version; the stable rebuild must also complete successfully.

Test and stable workflows share source packaging, signing and upload code.
The upload destination is derived from the channel and validated version;
test versions cannot be uploaded to stable by these scripts. Do not run
credentialed upload jobs from untrusted workflow branches. Source compilation
runs without publication credentials.

The devel repositories are not an installation channel for users. Existing
users must disable them before switching to stable; an installed historical
devel package may sort above a release and require an explicit downgrade.

## Stable COPR and Launchpad publication

The release form has `publish_copr` and `publish_ppa` switches, disabled by
default until credentials and repository settings are ready. After publishing
GitHub assets, selected jobs build distribution source packages from the exact
published tag and upload them to `subuk/vmango` and `ppa:subuk/vmango`.
Only stable `X.Y.Z` versions are accepted. The requested version is embedded in
a generated `VERSION` file in the source archive. Debian upload changelog entries
are generated automatically; the RPM spec version is substituted during packaging.
The workflow does not edit release commits.

`Publish distribution packages` can also be run directly for an existing
published release. Select either destination and a positive package `revision`
(default `1`). This does not recreate the GitHub release. Use this workflow or
**Re-run failed jobs** after a destination fails. Accepted versions are monitored
without uploading again; inspect remote logs before increasing `revision` for
a new package attempt. Never move the release tag to correct package contents:
code/packaging changes require a new release commit and version.

The source artifact contains an SRPM and Ubuntu source packages for Jammy,
Noble and Resolute. Debian versions are `VERSION-REVISIONppa1~SERIES`; source RPM
versions are `VERSION-REVISION.release`, with binary dist tags resolved by COPR.
CI builds these source artifacts without any publication credentials. It also
signs a temporary package copy with a disposable key and runs a simulated
`dput` upload, exercising signature checks without sending packages. It does
not establish that all remote distribution builds work.

COPR uses the fixed RHEL and EPEL 8/9/10 x86_64 target list. Both PPAs must have only
amd64 enabled; the uploader checks this before submitting packages. Publication jobs wait up to four hours: COPR must report
success, and Launchpad must complete every scheduled architecture build and
publish its `vmango` binary. Upload acceptance alone is not success. A timeout
fails the job but does not cancel remote builds; rerun to resume checking.
PPA rejection may only appear in the uploader's email, so inspect that inbox
if acceptance times out. After an ambiguous COPR upload/network failure, inspect
the project before rerunning, especially if source metadata is not visible yet.
Publication across GitHub, COPR and Launchpad is not atomic: a failed destination
does not roll back successful releases elsewhere.

### Configure GitHub credentials

In [repository Actions secrets](https://github.com/subuk/vmango/settings/secrets/actions),
add the following **repository secrets**. They are used only by publication jobs,
never by PR source builds. Do not paste their values into issues or PRs.

| Secret | Value |
| --- | --- |
| `COPR_CONFIG` | Complete INI configuration from the COPR API page, including `[copr-cli]`, `login`, `username`, `token`, and `copr_url` |
| `PPA_GPG_PRIVATE_KEY` | ASCII-armored private signing key registered to the Launchpad uploader |
| `PPA_GPG_PASSPHRASE` | Passphrase protecting that private key |

Obtain the COPR configuration by signing in as the project owner or a permitted
builder at <https://copr.fedorainfracloud.org/api/>. Copy the generated
configuration verbatim. Track its expiration and replace the secret when renewing.

For Launchpad, use a dedicated password-protected signing key whose email is
verified on the uploader's Launchpad account. The packaging identity currently
uses `Vmango release CI <admin@vmango.org>`. On your own machine:

```sh
gpg --quick-generate-key 'Vmango release CI <admin@vmango.org>' rsa4096 sign 1y
gpg --list-secret-keys --keyid-format LONG
# Replace FINGERPRINT with the complete fingerprint of this new key.
# Launchpad sends an encrypted ownership-verification email.
gpg --quick-add-key FINGERPRINT rsa4096 encr 1y
gpg --keyserver hkps://keyserver.ubuntu.com --send-keys FINGERPRINT
```

Register the fingerprint at <https://launchpad.net/~/+editpgpkeys> and complete
Launchpad's ownership verification. The uploader must have upload access to
`ppa:subuk/vmango`. Export the private key into a local private file:

```sh
umask 077
gpg --armor --output vmango-release-private.asc --export-secret-keys FINGERPRINT
```

Paste the complete file into `PPA_GPG_PRIVATE_KEY`, put its passphrase in
`PPA_GPG_PASSPHRASE`, then remove the temporary export after storing it securely.
This is the uploader's signing key, **not** the PPA repository signing key shown
on its overview page. GPG material is imported into a temporary runner directory
and removed after signing and upload verification. Uploads use `dput` and GPG signatures; no Launchpad
OAuth token, SSH key or GitHub PAT is required.

In [repository Actions variables](https://github.com/subuk/vmango/settings/variables/actions),
set:

| Variable | Value |
| --- | --- |
| `PPA_GPG_FINGERPRINT` | Full fingerprint of the uploaded signing key |

### Prepare stable repositories before the first upload

Stable and devel publication use six x86_64 targets:
`rhel-8-x86_64 rhel-9-x86_64 rhel-10-x86_64` and
`epel-8-x86_64 epel-9-x86_64 epel-10-x86_64`.
Both projects must have all six chroots enabled. EPEL 8/9 builds use RHEL + EPEL;
the COPR EPEL 10 target uses CentOS Stream + EPEL. Keep the separate RHEL targets
to verify builds against RHEL without EPEL build dependencies.
Keep RHEL 10 installation instructions pointed at `rhel-10-x86_64`: the EPEL 10
build can require libvirt symbols that are only available in CentOS Stream.
Each target needs Go >= 1.26; verify completed builds and configure
bootstrap/dependencies as needed. Preserve package signature checks.

Both PPAs target Ubuntu 22.04 (Jammy), 24.04 (Noble), and 26.04 (Resolute),
on amd64 only. Verify build dependencies, particularly `golang-1.26-go`, and
keep stable/devel compiler dependencies aligned. PPA does not build packages for Debian distribution suites. Instead, the
publication workflow installs the Ubuntu Jammy package on Debian 12/13 amd64
and checks startup, login and a libvirt test node. Keep the explicit Jammy
source in Debian instructions; do not substitute a Debian suite name in the
Ubuntu PPA URL. This check does not cover real KVM operations.

See [COPR submission methods](https://docs.copr.fedorainfracloud.org/user_documentation.html)
and [Launchpad source upload/signing](https://ubuntu.com/docs/launchpad/user/how-to/packaging/build-deb-packages-in-launchpad/).


## Embedded frontend in release sources

CI uses the Node version in `ui/.nvmrc`, runs `make ui`, checks generated API
types and runs frontend tests before archiving. The frontend is built from the
trusted source SHA. `ui/dist/build-manifest.json` records hashes of every input
and output, including source paths, the lockfile and OpenAPI snapshot.

Both `make tarball` and `scripts/release/package-sources.sh` include frontend
source, lockfile, runtime licenses and generated `ui/dist`. The latter appends
dist explicitly to `git archive HEAD` and verifies an extracted archive; ignored
build artifacts cannot be assumed to exist in a Git archive.

RPM and DEB compile/install/test targets use `UI_PREBUILT=1`, which checks the
manifest using Python 3 and never calls Node/npm. Debian clean/source-rebuild
also preserves packaged dist in this mode. Validate a source archive by extracting
it into a fresh directory and running `make UI_PREBUILT=1` and
`make UI_PREBUILT=1 test` with Node/npm absent and networking disabled. A changed
or missing source/asset must fail with an instruction to rebuild the frontend.
Do not disable digest checks to get a stale source archive through packaging.
