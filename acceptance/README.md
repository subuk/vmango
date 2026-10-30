# Real libvirt acceptance tests

This opt-in suite runs the built Vmango HTTP server against a real local or
explicitly selected remote libvirt hypervisor. It is excluded from `make test` and package builds. It creates a
randomly named `vmango-acceptance-*` storage pool and VM (2 vCPU, 2 GiB RAM,
8 GiB disk). It does not use your `vmango.conf` or modify existing VMs.

## Requirements

- Build dependencies from the main README, plus `virsh`, `qemu-img`, `mkisofs`
  and `isoinfo` on PATH.
- A working local `qemu:///session` connection, QEMU and suitable firmware.
  macOS requires HVF and EFI; Linux requires KVM (the x86 test uses the backend's default firmware). Software emulation fails the test.
- For local-file mode, an Ubuntu cloud image matching the host architecture,
  with cloud-init enabled. Managed mode uses the pinned Alpine URLs below.
- Enough space for the downloaded image, a converted copy and a cloned VM disk.
  QEMU user networking provides guest networking without a privileged bridge.

On Apple Silicon, install QEMU with `brew install qemu` in addition to the
main README's dependencies. Check `virsh -c qemu:///session domcapabilities
--arch aarch64` and confirm the `hvf` domain type and EFI support.

Download an ARM64 image from the official Ubuntu cloud image archive, and verify
its checksum against the manifest from the same dated directory. For example:

```sh
mkdir -p images
curl -fL -o images/noble-server-cloudimg-arm64.img \
  https://cloud-images.ubuntu.com/noble/20261017/noble-server-cloudimg-arm64.img
curl -fL -o images/SHA256SUMS \
  https://cloud-images.ubuntu.com/noble/20261017/SHA256SUMS
(cd images && shasum -a 256 -c <(grep ' \*noble-server-cloudimg-arm64.img$' SHA256SUMS))
```

For x86_64, use the `amd64` image instead. Image downloads are deliberately
separate from the test, so subsequent runs work with the cached image.

```sh
ACCEPTANCE_IMAGE="$PWD/images/noble-server-cloudimg-arm64.img" make acceptance
```

`ACCEPTANCE_URI=qemu:///system` is also accepted for local Linux hosts. The
libvirt/QEMU account must be able to access the test's `result/` directory;
the default session connection runs as your user and needs no permission changes.
For an explicitly selected remote libvirt connection, use the downloadable-image mode below.

## What constitutes success

The test logs in through the JSON endpoint, loads the SPA creation shell and
rejects an insufficient
RAM request before creating a disk, and exercises two catalog IDs referencing
one source disk. It creates and deletes a VM with initialization disabled, then
clones the image and
creates the VM and config drive through the JWT Bearer-authenticated JSON API. Mutations assert 202 acceptance,
then poll the durable task until success before inspecting the real backend.
The harness uses a dedicated task directory under its own artifact directory.
It also checks API discovery, duplicate-create conflicts and partial updates. It checks the image-selected SCSI/virtio-scsi configuration, explicit guest agent
disablement and presence/absence of the config drive. It checks the generated domain
uses hardware acceleration, starts it through HTTP and checks libvirt reports
it running. It then checks the VNC WebSocket proxy's RFB greeting and reads the
serial WebSocket until cloud-init runs the supplied guest command, verifies the
hostname and prints a unique marker. Merely reaching libvirt's `running` state
is not sufficient.

It reconnects through signed serial/VNC links and verifies a second serial
client gets a conflict without explicit force.

Finally it powers off the guest and deletes the VM and its attached disks through
the JSON API, verifying they disappeared. `poweroff` is Vmango's hard power-off action;
this does not test graceful shutdown. The suite does not yet test reboot, SSH,
guest-agent integration, browser interaction, external networking or Linux KVM
when run on a Mac.

To exercise the OpenStack config drive instead of NoCloud:

```sh
ACCEPTANCE_INITIALIZATION=openstack ACCEPTANCE_IMAGE=/absolute/path/cloud.img make acceptance
```

## Running on a shared Linux host

Run the suite locally on the target host, using `qemu:///system` if necessary.
Supply a separately downloaded test image; do not reuse existing VM disks.
Build artifacts can be transferred from another Linux build environment; place
`bin/vmango` and a compiled acceptance test binary in the same project layout,
and run the test binary with `acceptance/` as its working directory.

Use a dedicated working directory accessible to the libvirt/QEMU account.
Do not change the host's existing Vmango configuration, pools, networks or VMs.
Record the exact resources created by each run and only remove those resources.
Never infer ownership from a prefix alone or perform wildcard cleanup. Inspect
cleanup on partial failures as well as successful runs. Report Linux/KVM and
macOS/HVF results separately.

## Logs and cleanup

Each run prints its artifact directory under `result/`: `vmango.log`,
`domain.xml` and `console.log`. This directory and `images/` are ignored by Git.
The temporary web credentials are random, the server binds only to loopback,
and its configuration is removed after the run.

On both success and ordinary test failures, cleanup destroys/undefines only the
randomly named test VM and pool and removes that pool's files. A manually downloaded
source image and logs remain. Managed source volumes are removed with the owned
test pool. Cleanup errors fail the test. Killing the test
process or its overall timeout may prevent cleanup; use the exact resource name
from the printed artifact path to inspect/remove leftovers with `virsh`. Never
use a wildcard deletion against a shared libvirt host.

## Lazy managed images (local or remote)

To verify the production HTTP → Vmango → libvirt streaming path, supply a direct
QCOW2 URL and its SHA-256 instead of `ACCEPTANCE_IMAGE`. No image payload is
prepared or cached on the Vmango side. The test creates a dedicated `dir` image
pool, confirms discovery leaves it empty, prepares the source in a durable
create task, and reuses it through a second catalog ID before booting the guest.
Its random pool/VM and ownership checks are the same as the local-file suite.

Local Apple Silicon example using the pinned shipped Alpine image:

```sh
ACCEPTANCE_IMAGE_URL=https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/cloud/alpine-3.24.3-aarch64-cloudinit-r0.qcow2 \
ACCEPTANCE_IMAGE_SHA256=e83922eefe35a28eb5380fdb2ea0845c6a33c83be44a688ad08932014ceac3d2 \
make acceptance
```

Remote Linux KVM from a Mac/Linux Vmango process:

```sh
ACCEPTANCE_REMOTE=1 \
ACCEPTANCE_ARCH=x86_64 \
ACCEPTANCE_URI=qemu+ssh://root@your-test-host/system \
ACCEPTANCE_IMAGE_URL=https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/cloud/alpine-3.24.3-x86_64-cloudinit-r0.qcow2 \
ACCEPTANCE_IMAGE_SHA256=946677073fbd8e6345a8f150ce4e1848e10bbb3daef8f05f6c26dcea0d82db23 \
make acceptance
```

Remote mode is explicit, requires a downloadable image and target architecture,
and permits only a `qemu+ssh` URI. It creates its own random pool directory under
`/tmp` **on the destination node** using libvirt. It needs permissions for pool,
volume and domain operations; SSH keys must already work. Cleanup verifies the
pool UUID and volume paths, deletes volumes only inside that owned test pool,
and removes its directory through libvirt. No existing pool or VM is reused.
Application-side task/auth/config metadata and logs remain under `result/`.

Successful create tasks are also saved as `create-task-<uuid>.json`. The first
records streaming byte/speed/ETA samples and verification; the second confirms
reuse without another transfer stage. Boot success requires cloud-init to print
the unique marker and verify its hostname. The marker uses the explicit
`ttyAMA0`/`ttyS0` serial device because Alpine's `/dev/console` may select graphics.

### Recorded verification, 2026-10-30

| Artifact | Vmango → backend | Firmware / initialization | Result |
| --- | --- | --- | --- |
| Alpine 3.24.3 r0 cloud-init, aarch64, SHA-256 `e83922ee…ceac3d2` | macOS → local libvirt/HVF | UEFI / NoCloud | Download, SHA-256, backend confirmation, reuse, VM boot, cloud-init hostname/command, VNC/serial and cleanup passed |
| Alpine 3.24.3 r0 cloud-init, x86_64, SHA-256 `94667707…82db23` | macOS → remote Linux libvirt/KVM | Backend default BIOS / NoCloud | Same flow passed through remote volume upload |

The local and remote runs took about 48 and 226 seconds respectively. Full logs
remain in the run-specific `result/` directories. Linux/KVM was verified separately
from macOS/HVF. Publisher checksum/header/HTTP validation covered all twelve
shipped artifacts; real guest boot/provisioning covered only these Alpine variants.
No package build, package installation, other distribution guest, or other
architecture/backend combination is established by these checks.
