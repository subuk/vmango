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

GitHub binary releases can optionally publish stable COPR and PPA packages using
the jobs described below. To build packages locally, build RPMs
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


## Stable COPR and Launchpad publication

The release form has `publish_copr` and `publish_ppa` switches, disabled by
default until credentials and repository settings are ready. After publishing
GitHub assets, selected jobs build distribution source packages from the exact
published tag and upload them to `subuk/vmango` and `ppa:subuk/vmango`.
Only stable `X.Y.Z` versions are accepted. The Makefile and package changelogs
must already contain that version; the workflow does not edit release commits.

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
CI builds these source artifacts without any publication credentials. It does
not establish that all remote distribution builds work.

COPR uses only the explicit `COPR_CHROOTS` list. Launchpad uses architectures
enabled in the PPA. Publication jobs wait up to four hours: COPR must report
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
and removed after signing. Uploads use `dput` and GPG signatures; no Launchpad
OAuth token, SSH key or GitHub PAT is required.

In [repository Actions variables](https://github.com/subuk/vmango/settings/variables/actions),
set:

| Variable | Value |
| --- | --- |
| `PPA_GPG_FINGERPRINT` | Full fingerprint of the uploaded signing key |
| `COPR_CHROOTS` | `rhel-9-x86_64 rhel-10-x86_64` |

### Prepare stable repositories before the first upload

Stable publication targets RHEL 9 and RHEL 10 on x86_64 only:
`rhel-9-x86_64 rhel-10-x86_64`. These chroots were enabled in stable
`subuk/vmango` on 2026-10-29. Historical EPEL repositories remain available,
but the release workflow does not submit new builds to them. Development
project targets are configured independently. Each selected target needs
Go >= 1.26; verify completed builds and configure bootstrap/dependencies as
needed. Preserve package signature checks.

For stable PPA, verify Jammy/Noble/Resolute availability, enabled architectures
and build dependencies, particularly `golang-1.26-go`. Add the appropriate trusted
compiler PPA dependency where Ubuntu does not provide it. Development recipe
settings do not automatically configure the stable PPA. The existing
`master-daily` recipe and `make publish-devel` remain unchanged.

See [COPR submission methods](https://docs.copr.fedorainfracloud.org/user_documentation.html)
and [Launchpad source upload/signing](https://ubuntu.com/docs/launchpad/user/how-to/packaging/build-deb-packages-in-launchpad/).
