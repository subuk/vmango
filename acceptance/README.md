# Real libvirt acceptance tests

This opt-in suite runs the built Vmango HTTP server against a real local or
explicitly selected remote libvirt hypervisor. It is excluded from `make test` and package builds. It creates a
randomly named `vmango-acceptance-*` storage pool and VM (2 vCPU, 2 GiB RAM,
8 GiB disk). It does not use your `vmango.conf` or modify existing VMs.

## Requirements

- Build dependencies from the main README, plus `virsh`, `qemu-img`, `mkisofs`
  and `isoinfo` on PATH.
- A working local `qemu:///session` connection, QEMU and suitable firmware.
  macOS requires HVF and EFI; Linux requires KVM (the x86 test uses the backend's default firmware). Software emulation is rejected by default; the explicit QEMU test mode below can verify it separately.
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
one source disk through the configured cloud. It creates and deletes a VM with initialization disabled, then
clones the image and
creates the VM and config drive through the JWT Bearer-authenticated JSON API. Mutations assert 202 acceptance,
then poll the durable task until success before inspecting the real backend.
The harness uses a dedicated task directory under its own artifact directory.
It also uses a dedicated cloud-init template catalog: the booting guest receives
user-data selected through the template API, and deleting that template after
provisioning does not alter the guest configuration.
It also checks API discovery, duplicate-create conflicts and rejection of the removed machine PATCH route. It checks administrator-owned autostart during an explicit type change and a loopback graphics binding. It checks the image-selected SCSI/virtio-scsi configuration, explicit guest agent
disablement and presence/absence of the config drive. It checks the generated domain
uses the expected virtualization type (hardware acceleration by default), starts it through HTTP and checks libvirt reports
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

For the legacy x86 power-management regression, add `ACCEPTANCE_ADOPT_POWER=1`
to an x86_64 run with a guest that handles ACPI power-button events, such as
Ubuntu cloud. The test keeps the guest agent disabled, so a minimal image without
an ACPI event handler will time out on shutdown even with correct virtual hardware.
Before booting its isolated VM, the suite removes ACPI/APIC,
sets poweroff/crash to restart and removes management metadata. It then adopts
the VM through HTTP, checks the repaired persistent settings, boots the guest,
requests graceful shutdown and verifies it stays off. It also starts it again
and checks hard power-off. This mode never changes existing user machines.

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

## Explicit software-emulation checks

For disposable Linux hosts without nested KVM, set `ACCEPTANCE_VIRT_TYPE=qemu`.
This changes the expected domain XML type in the test only. It does not add a
Vmango configuration option or force libvirt to choose another backend. The host
must provide a QEMU binary with software emulation and libvirt must report
`qemu` domain capabilities. If its QEMU build only supports KVM, installation of
libvirt packages alone does not make the emulation test possible.

Set the image, URI, architecture and remote-mode variables using the examples
below, and run:

```sh
make
ACCEPTANCE_VIRT_TYPE=qemu ACCEPTANCE_BOOT_TIMEOUT=20m \
  go test -mod=vendor -tags=acceptance -count=1 -v -timeout=30m ./acceptance
```

`ACCEPTANCE_VIRT_TYPE` accepts `kvm`, `hvf` or `qemu`. Omission retains the
existing Linux/KVM and local macOS/HVF expectations. `ACCEPTANCE_BOOT_TIMEOUT`
controls the serial-console wait for the cloud-init marker; it defaults to five
minutes and accepts durations from one second through twenty minutes. The test
process's overall timeout must allow provisioning, the selected boot wait and
cleanup; `make acceptance` retains its ordinary fifteen-minute limit.

The same guest, storage, config-drive and console assertions run in QEMU mode.
A pass establishes software-emulated functionality, not hardware KVM or nested
virtualization. Installer acceptance on the reusable disposable hosts will use
this mode where supported; this paragraph records the test procedure, not a
claim that those hosts have passed it.


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
ACCEPTANCE_IMAGE_SIZE_BYTES=191823872 \
make acceptance
```

Remote Linux KVM from a Mac/Linux Vmango process:

```sh
ACCEPTANCE_REMOTE=1 \
ACCEPTANCE_ARCH=x86_64 \
ACCEPTANCE_URI=qemu+ssh://root@your-test-host/system \
ACCEPTANCE_IMAGE_URL=https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/cloud/alpine-3.24.3-x86_64-cloudinit-r0.qcow2 \
ACCEPTANCE_IMAGE_SHA256=946677073fbd8e6345a8f150ce4e1848e10bbb3daef8f05f6c26dcea0d82db23 \
ACCEPTANCE_IMAGE_SIZE_BYTES=157810688 \
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

## Cloud scheduling verification

Creation now uses `/api/v1/clouds/acceptance/machines`, an instance type,
logical network and storage class. The harness configures its own cloud, zone
and host bindings. A second cloud uses a deliberately unreachable socket and
an independent definition of the same image ID. Its configuration and image
pages must remain readable while live host/volume inventories explicitly report
unavailability. The suite verifies that editing the configuration file leaves
the running accepted snapshot unchanged and that the eight retired host API
routes and old host UI routes return 404.

Volume checks use flat cloud-scoped routes and canonical compound host/path
IDs. They verify cross-cloud rejection, rejection of old routes/path-only IDs,
resource-detail catalogs and consistent VM disk references. After a completed
attach task, the suite restarts its own Vmango process and checks that task
history, real references and volume navigation survive unchanged. This restart
checks completed real-backend operations; the separate
`TestPreCloudImageCommandRecoversPinnedSourceAndRawLocks` regression uses a
persisted pre-change command payload and simulated backend to check queued
recovery with accepted image sources/defaults and original locks.

It also verifies stopped-machine type changes preserve boot
disk size, explicit adoption preserves resources, and unmanaged commands are
rejected before mutation. Existing user domains remain part of fresh capacity
accounting, including inactive domains.

On a shared host whose existing requests already exceed physical capacity,
`ACCEPTANCE_CAPACITY_REJECTION=1` runs a separate negative admission check.
Use it with the pinned downloadable-image settings above and the chosen URI.
It expects a failed task without uncertain effects, no VM/root disk/image
provisioning, and cleans its own random test pool. It does not run or establish
a successful VM lifecycle. Never reduce existing workloads or bypass accounting
to make this test pass.

KRG-54 verification on 2026-10-30: the local macOS/HVF cloud lifecycle passed.
On `lanst.net.krglv.com`, real Linux/KVM admission correctly rejected placement:
12 host CPUs versus 26 configured legacy vCPUs across running and inactive VMs.
Only the negative admission check passed there; a successful Linux cloud
lifecycle still requires a host with sufficient allocatable capacity. Logs are
retained under `result/krg54-linux/`; original workloads were preserved.

## Administration and volume migration verification, 2026-10-31

The KRG-68 harness passed locally on macOS/libvirt/HVF with both the existing
Ubuntu ARM64 local cloud image (27.61 seconds) and the pinned Alpine 3.24.3
ARM64 managed image (61.55 seconds). Both runs checked the offline cloud's
accepted configuration, independent same-ID image definitions, removed routes,
compound volume addresses, resource-detail catalogs, guest cloud-init boot,
VNC/serial, disk operations, process restart/history preservation and cleanup.
The managed run reported the verified source ready; its saved task records
showed one 191,823,872-byte stream and verification followed by reuse without a
second transfer. Final inspection showed the original user machines and default
pool unchanged and no acceptance VM/pool remaining.

The corresponding ignored artifact directories are
`result/vmango-acceptance-a42c5506-77a7-4279-9f0c-f68450efb9d6/` and
`result/vmango-acceptance-73847964-55ae-44a0-a020-71656cf42145/`.
This verification establishes neither Linux/KVM or remote behavior nor package
builds/installations. Browser checks are separate from the lifecycle harness.

## Namespace lifecycle and migration verification

The harness configures `platform` and `analytics` against the same isolated
cloud/host; `platform` also admits the deliberately offline cloud. Namespace
selection is explicit on workload HTTP requests and native console handshakes.
All namespace assignments, keys, templates and task history use the run's own
application directories. The test never edits the installation configuration.

The namespace checks cover:

- Missing, combined and unknown context, cloud admission, and rejection of API
  query selectors; bootstrap and Administration remain context-free.
- Equal machine and volume display labels in different namespaces, with distinct
  backend identities; cross-namespace reads, mutations, disk attachment, template
  lookup and task lookup fail before effects.
- Equal SSH fingerprints with independent catalog entries, and preservation of
  guest hostname behavior despite generated backend names.
- Restart persistence, removal and re-addition of namespace configuration, and
  read-only Administration access to preserved assignments and history.
- Offline CLI migration with an explicit VM/disk/key/template mapping: dry-run
  leaves XML unchanged; apply creates a private backup; reapplying the same
  manifest succeeds without changing IDs, template revisions, disks or historical
  task attribution. Assignment does not grant deletion ownership.

Cleanup tracks exact VM UUIDs recorded in accepted tasks, verifies their disk
paths against the owned pool, and checks the pool UUID before removing it. It
never uses a display label or a shared prefix to infer resource ownership.

Recorded on 2026-11-04: the namespace harness passed with the existing local
Ubuntu ARM64 cloud image on macOS/libvirt/HVF (38.52 seconds). This run also
verified guest cloud-init, native and signed serial/VNC connections, disk
operations and cleanup. Artifacts remain in
`result/vmango-acceptance-474cad44-83bc-4f87-b566-5309006a011f/`, including the
migration backup and effects. The tested storage combination is a local `dir`
pool with QCOW2 volumes. This result does not establish remote libvirt,
read-only connections, Linux/KVM, other storage combinations, browser rendering
or package builds/installations.

## Storage class cloning and shared bases

The lifecycle suite also creates small, nonzero raw and qcow2 volumes in its
owned pool. For each source it creates two larger overlays, checks their explicit
backing path/format and capacity, rejects nested overlays and base mutation,
writes a distinct child-only block, and copies that overlay into an independent
disk. It then deletes the complete
source chain and reads the independent copy to verify preserved data. Base
contents are compared before and after the operations. With a test VM present,
starting that VM with a referenced base attached writable must fail.

Set `ACCEPTANCE_OVERLAY=1` to boot the main guest from the `efficient` storage
class. The suite verifies the actual backing metadata, guest cloud-init marker,
and unchanged source image bytes after guest shutdown. The ordinary mode keeps
booting an independent copy. Both modes run the storage checks above.

On an authorized host where existing allocations prevent VM admission, set
`ACCEPTANCE_STORAGE_ONLY=1` with the normal local/remote image settings. This
runs only the volume matrix through the HTTP API and libvirt; it creates no
VM and does not bypass admission. It does not establish guest boot or start
protection on that host. Run `ACCEPTANCE_CAPACITY_REJECTION=1` separately to
record admission failure. Existing workloads must never be changed to make
these tests fit. Logs and downloaded comparison images stay under the run's
`result/` directory; only that run's exact pool and volumes are cleaned up.

Recorded on 2026-11-07: the local macOS/libvirt/HVF overlay lifecycle passed
with the Ubuntu ARM64 cloud image (62.63 seconds), including create-and-start,
explicit start, guest cloud-init, unchanged source bytes, both raw/qcow2 storage
matrices, referenced-base start protection, process restart and cleanup. Artifacts
remain in `result/vmango-acceptance-56a65ac6-a77e-4425-8ac0-37912b14052b/`.
A subsequent storage-only run also verified specific dependency-conflict
explanations and preservation of child-only changed blocks (9.83 seconds), under
`result/vmango-acceptance-be1e3098-4bdd-4d11-9ad5-19feeb4a3e91/`.
The existing local VMs were preserved; the test used the existing default
admission policy without overriding host allocation settings.

Run suites against the same libvirt host sequentially. Another suite's cleanup
can remove its pool during a host-wide dependency scan; the affected operation
then correctly fails without mutation because the scan is incomplete.

The remote Linux/libvirt/KVM overlay lifecycle on `lanst.net.krglv.com` passed
separately with the pinned Alpine x86_64 managed image (276.00 seconds), including
image streaming/reuse, both startup paths, guest cloud-init, unchanged base
contents, raw/qcow2 volume checks, protected-base startup, restart, migration
and cleanup. Artifacts remain in
`result/vmango-acceptance-6d997e41-3413-476a-a619-cbc65df260f2/`.
No CPU overcommit setting was changed: both hosts used the application's
existing default admission policy. These results establish the tested `dir`
pools and guests, not other storage backends or package builds/installations.
A final Linux storage-only run verified specific conflict explanations and
preserved child-only changed blocks after source-chain deletion for both source
formats (69.60 seconds), with artifacts in
`result/vmango-acceptance-75e65605-eb3d-4463-bae2-4281032cf95a/`.
Final read-only inventories confirmed all original VM states, allocations and
autostart settings were unchanged and no acceptance VMs or pools remained.
