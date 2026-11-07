# Web setup wizard

`vmango install` opens a temporary web wizard beside the Vmango server that
will use the resulting configuration. It prepares Linux libvirt hosts, reviews
network and storage changes, and writes the explicitly selected configuration
file. It does not install, relocate, start or restart Vmango, or create a
systemd or launchd service for Vmango.

## Controller and target scope

| Controller | Hypervisor targets |
| --- | --- |
| macOS | Linux hosts over SSH |
| Linux | Local Linux, Linux hosts over SSH, or both |

The initial recipes cover Ubuntu 24.04 and RHEL-compatible 10 on x86_64 and
AArch64. Recipe selection is explicit in Vmango using freshly observed OS
information. A recipe's presence is not a claim that every distribution,
architecture and controller combination has passed live acceptance.

The wizard manages persistent IPv4 NAT networks and directory (`dir`) storage
pools. It can reuse compatible existing resources. It does not create physical
interface bridges, partition or format disks, configure LVM/Ceph/shared storage,
move VMs, or arrange migration or overlay networking. Identical network or pool
names on two hosts do not make their networks or storage shared.

Local macOS QEMU/HVF setup and Windows controllers are outside this command's
scope. The ordinary Vmango server's existing platform support is unchanged.

## Supply the pinned Richt executable

Richt runs as a separate executable, not as a Vmango Go dependency. Provide its
path with `--richt`; if omitted, Vmango searches `PATH`. The installer does not
download or upgrade that executable. The supported source revision is
`43f24f90aae04766f8c5979b06ce08cb33e703eb`, with saved-plan format `1.8`.
A generic development build or a differently versioned binary is rejected.

To build the pinned executable for the current macOS or Linux controller, use
Richt's required Go toolchain (Go 1.27.3 for this revision):

```sh
git clone https://github.com/subuk/richt.git richt-installer-runtime
cd richt-installer-runtime
git checkout --detach 43f24f90aae04766f8c5979b06ce08cb33e703eb
make build VERSION=43f24f90aae04766f8c5979b06ce08cb33e703eb
./bin/richt --version
```

Pass the resulting `bin/richt` as an absolute path. This build requirement
belongs to Richt; it does not raise Vmango's own minimum Go version. A binary
built by another trusted machine must match the controller's OS and CPU
architecture. Vmango records its digest and rejects replacement during the
session or incompatible continuation.

Recipe source bytes live under `installer/recipes/`. Preparation references
immutable HTTPS sources at the pinned repository revision and checks each
SHA-256. A missing source or checksum mismatch fails preparation. Review and
publication of recipe changes must update their source pin; a routine installer
rerun does not silently fetch newer recipes.

## Prerequisites and identities

The controller needs the normal Vmango runtime libraries, OpenSSH, the pinned
Richt executable and the ISO tools used for config drives. The browser needs no
Node.js runtime; the wizard is included in Vmango's verified embedded assets.

Targets need Linux with systemd, Python 3 and iproute2 with JSON output before
inspection. The selected recipes install their QEMU/libvirt and supporting
packages after approval. Package operations need root or explicitly selected
noninteractive `sudo -n`. SSH password prompts, interactive sudo and automatic
host-key acceptance are not supported.

Use the controller's existing SSH configuration, keys and agent. Establish host
trust with the normal SSH workflow first. Supply the SSH hostname or alias,
optional user and port in their respective fields. An SSH alias can use the
normal `ProxyJump` configuration. No SSH password or private key is copied into
the wizard.

Run the controller as the identity that will run Vmango when possible. The
prepared libvirt access must work from that identity with its lasting SSH keys,
`known_hosts` and libvirt URI. Preparation through sudo does not prove that a
separate future service account can connect. Test the intended service identity
before activating the resulting server configuration. Console connectivity is
also checked separately during full VM acceptance.

## Start the wizard

Choose an existing writable parent directory and an explicit config path:

```sh
vmango install --config /absolute/path/vmango.conf \
  --richt /absolute/path/richt
```

`--config` is mandatory for install mode; an environment-variable default does
not replace the explicit choice. The browser cannot choose another destination.
An existing configuration is read and validated. A missing file starts the
first-run form and is not created until the final approval. Permission errors
and invalid configuration are errors, never a request to overwrite with defaults.

The listener defaults to `127.0.0.1:0`, selecting a free local port. Open the
printed session URL, which includes a private token. Do not share that URL.
The installer has its own session and origin/host protections and does not
require an already configured administrator account. Installer mutation routes
are not mounted by the normal `vmango web` server.

For a remote controller, use the printed SSH-tunnel command from the browser
machine. Keep the same local port and open the printed loopback URL. The
`--listen` option accepts loopback IP addresses only; it is not a way to expose
an unauthenticated setup page publicly.

Optional command settings:

- `--state-dir /absolute/private/directory` selects recovery storage.
- `--parallelism N` bounds host execution; the default is three.
- `--listen 127.0.0.1:PORT` fixes the temporary browser endpoint.

Root is unnecessary merely to run a macOS or remote-only controller. The
configuration file and its parent directory must be writable by the controller
identity. The wizard does not elevate privileges to write that local file.

## Review three separate stages

1. **Prepare the operating systems.** Select hosts, inspect them and review
   packages, libvirt access membership, directories and daemon/socket settings.
   The UI runs `richt diff`, saves its executable plan and displays plain text.
   Approval runs `richt apply -y` against that exact saved plan.
2. **Prepare libvirt resources.** After the daemons are available, Vmango
   observes the networks and pools and shows concrete create/start/autostart
   actions. Approve these separately. Compatible active resources are reused;
   incompatible or occupied resources produce conflicts instead of deletion or
   redefinition.
3. **Publish Vmango configuration.** Review the final host selection and safe
   configuration summary. Vmango validates and writes the local file itself.
   Richt does not write the Vmango configuration.

A new cloud starts with a `small` instance profile: two vCPUs, 2048 MiB RAM
and an 8 GiB boot disk, matching the minimum disk size of its pinned Ubuntu
24.04 images. The initial catalog includes x86_64 images using the host's default
firmware and AArch64 UEFI images, with NoCloud initialization. The final review
lists these additions; existing
instance profiles and image catalogs are left unchanged. API changes show the
old and new listener, public URL and TLS file paths, plus whether administrator
credentials change. Password hashes and certificate/private-key contents are
never included in that review.

Shared network/storage defaults can be overridden per host. Paths belong to the
machine indicated in the form: a pool path is on its Linux hypervisor, while
the configuration and API certificate paths belong to the Vmango controller.

To reuse resources with different settings, enter their network and pool names,
then choose **Load existing resource settings**. The wizard saves the current
answers and reads the selected hosts through libvirt. For compatible existing
resources it loads the subnet, DHCP range and pool directory into the form;
missing resources keep the desired values you entered. This action changes no
host resources or configuration files and invalidates any pending approval.
Review the loaded values before preparing another plan. A host whose libvirt
daemon or runtime access is not ready reports its own error; prepare that host
and retry the load when access is available.

Network creation uses a canonical IPv4 subnet and its first usable address as
the gateway. DHCP endpoints must be ordered and exclude the network address,
gateway and broadcast. Initial support requires a single compatible IPv4 NAT
subnet; bridge conversion and more complex existing network configurations must
be managed separately. Pools need clean absolute directory paths and cannot be
repointed to another directory. The OS stage prepares missing directories; the
libvirt stage never formats storage or deletes disks.

API access can use loopback/SSH tunnels, an existing HTTPS proxy, or native
HTTPS with supplied certificate and key paths. Public access requires an HTTPS
URL. Supply an initial administrator password when needed. Password hashes,
private candidate configuration bytes and executable plans are not browser
responses. Existing password hashes and signing-key paths remain unchanged
unless the corresponding setting is explicitly changed.

Editing answers invalidates the pending approval. Further discoveries do not
silently add actions to an approved plan. One failed host does not erase the
results of independent hosts. To finish with only successful hosts, explicitly
select and review that final subset. Existing unselected or unavailable host
entries are not removed from the configuration.

## File preservation and activation

The editor makes bounded changes to supported native HCL settings and additions.
It preserves unrelated blocks, comments, existing host identifiers and URIs,
images, namespaces, authentication and state/key paths. Ambiguous duplicate
settings, JSON-form HCL, existing host relocation and unsupported conversions
produce an actionable error rather than a full-file regeneration.

The destination must be a regular file, not a final symlink or hardlink. Parent
directory links are resolved at startup and checked again before publication.
The parent directory must already exist. The installer locks the selected
configuration against other installer sessions; it does not lock arbitrary
editors, so finish external edits before approving publication.

Immediately before replacement, Vmango checks that the original contents,
permissions and ownership still match the prepared expectation. Existing
configuration bytes receive a private durable backup named
`CONFIG.backup-SHA256`. A temporary file on the same filesystem is validated,
synced and atomically renamed over the destination. Existing permissions,
ownership and supported extended attributes are retained; new configurations
use mode `0600`. A no-op creates no replacement or additional backup.

The completion page includes host outcomes, the config path and any backup
path. Activate it through the normal server command or existing service:

```sh
vmango web --config /absolute/path/vmango.conf
```

Updating the file does not hot-reload a running server. Restart an existing
service through its normal administration procedure when ready.

## Interruption and recovery

Approved work belongs to the installer process, not to an HTTP request. Closing
and reopening the browser reconnects to that work. Use explicit cancellation
when required. Stopping an SSH client cannot guarantee that a command already
accepted on a target stopped; the result is reconciled before continuation.

By default recovery files are kept beneath the controller's user cache directory
at `vmango/install/<config-path-digest>/`. The directory is private (`0700`) and
plans/state files are private (`0600`). They can contain real secret-bearing
bytes. Preserve them for recovery and do not attach them to public issues.

After process interruption, rerun the command with the same config path, Richt
binary and state directory. Review the recovered stage and explicitly resume.
Richt skips completed tasks in the saved plan; Vmango re-observes unfinished
libvirt work and verifies resource identities before retrying. Changed
configuration, binary or plan inputs fail instead of silently using stale
approval. If a fresh reconciliation is necessary, retain the old evidence and
choose another private `--state-dir`.

Saved-plan continuation does not refetch recipe/templates or reevaluate their
preparation inputs. Package transactions can still require network access.
There is no automatic global rollback of packages, resource creation or config
history. After a completed run, the current configuration and actual host state
are inspected again instead of treating old answers as authoritative.

## Verification scope

Use the ordinary Go/UI tests and a rendered-browser check for the session,
review and reconnect flows. Real acceptance must use separately authorized
disposable hosts and isolated networks, pools, volumes and VMs. See
[real libvirt acceptance](../acceptance/README.md) for the explicit software
emulation test mode on hosts without nested KVM. The wizard introduces no new
Vmango acceleration setting; libvirt selects the existing supported backend.

For a guest lifecycle check against the exact installer output, start the normal
server with that configuration and run the focused script below. Set
`VMANGO_TEST_USER` and `VMANGO_TEST_PASSWORD` in the environment using its test
administrator account. Supply the actual libvirt network name in
`--backend-network`; `--network` selects the separate cloud catalog ID.

```sh
python3 scripts/test-installer-lifecycle.py \
  --config /absolute/path/vmango.conf --url http://localhost:8080 \
  --cloud local --zone local --network lan \
  --backend-network vmango-test-network \
  --output /absolute/path/new-private-evidence-directory
```

The script creates a unique test VM, checks its disks, boot, guest network,
serial console and VNC connection, then deletes only that VM and its attached
disks. It preserves networks, pools and image caches. It records the configuration
digest and verifies that the file stays unchanged. Select `--image` explicitly
when the catalog does not use the initial x86_64 image ID.

Record the actual controller/target versions and report local tests, browser
checks, package builds, OS provisioning and guest lifecycle checks separately.
A successful software-emulated guest establishes neither hardware KVM nor
nested-virtualization support. Pending acceptance runs are not successful tests.
