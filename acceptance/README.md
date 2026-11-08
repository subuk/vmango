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
It also checks API discovery, duplicate-create conflicts and rejection of the removed machine PATCH route. It checks lifecycle-managed autostart after create/start/stop and its preservation during an explicit type change and a loopback graphics binding. It checks the image-selected SCSI/virtio-scsi configuration, explicit guest agent
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

For the x86 adoption and graceful-shutdown regression, add
`ACCEPTANCE_ADOPT_POWER=1` to an x86_64 run with a guest that handles ACPI
power-button events, such as Ubuntu cloud. The test keeps the guest agent disabled,
so a minimal image without an ACPI event handler can time out. The suite removes
management identity only from its isolated VM, explicitly adopts it with the
compatible `boot` hardware profile and verifies that hardware and power settings
are unchanged. It then boots the guest, requests graceful shutdown and confirms
that the VM stays off and autostart is disabled. It starts again and checks hard
power-off. It never changes existing user machines.

To exercise the profile-owned OpenStack config drive instead of NoCloud:

```sh
ACCEPTANCE_INITIALIZATION=openstack ACCEPTANCE_IMAGE=/absolute/path/cloud.img make acceptance
```

The default attachment is a virtio disk. To verify another supported attachment
and actual guest discovery, combine `ACCEPTANCE_CONFIG_DRIVE_BUS=scsi` with
`ACCEPTANCE_CONFIG_DRIVE_DEVICE=disk` or `cdrom`. `ide` requires a compatible host
platform. For example:

```sh
ACCEPTANCE_CONFIG_DRIVE_BUS=scsi ACCEPTANCE_CONFIG_DRIVE_DEVICE=cdrom \
ACCEPTANCE_INITIALIZATION=openstack ACCEPTANCE_IMAGE=/absolute/path/cloud.img make acceptance
```

Each run validates the attachment type/bus and absence of firmware boot priority,
then requires the guest's cloud-init marker. XML validation alone is not a guest
discovery result. Report each tested format/attachment/platform combination.

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
This sets the test profiles' explicit `machine.acceleration` policy and checks
the resulting domain XML type. It requires software emulation without falling
back to another backend. The host
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

On a shared host whose existing requests already exceed its configured CPU
admission budget (physical CPUs after reserve multiplied by CPU overcommit),
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

## Machine binding verification

`ACCEPTANCE_MACHINE_TYPE` selects the disposable profiles' machine requirement;
omission uses `auto`. For example, combine `ACCEPTANCE_MACHINE_TYPE=q35` with
`ACCEPTANCE_VIRT_TYPE=kvm` on an eligible Linux x86_64 host, or use `virt` and
`hvf` on macOS ARM64. A supported exact name can test version pinning.
The suite checks concrete machine/emulator bindings and preserves the machine,
domain type and emulator across a resource change. Real boot still requires the
guest cloud-init marker, not just XML acceptance. Report Linux/KVM, macOS/HVF
and explicit TCG results separately; simulated capability removal and alias
changes are covered by focused unit tests.

### Recorded machine-policy verification, 2026-11-08

The isolated Ubuntu 24.04 ARM64 lifecycle passed on macOS with explicit
`machine { type="virt" acceleration="hvf" }` (49 seconds), and again with the
new `hardware` acceleration default (42 seconds). Both resolved `virt-9.2` and
preserved the concrete machine/domain/emulator through the resource-change
check. Explicit software emulation with `type="virt-9.2" acceleration="qemu"`
also passed the full guest cloud-init/VNC/serial/storage/restart/cleanup flow
(387 seconds). TCG timing and CPU/device support are distinct from HVF.

Artifacts remain under `result/vmango-acceptance-65c6d94e-ee8a-47fd-bed9-16146212b4d5/`
(explicit HVF), `result/vmango-acceptance-ce367274-58df-4f1c-98c4-4e2240d02bf1/`
(hardware default), and `result/vmango-acceptance-6e4c4103-3fb2-467c-9ed9-1d7b4932a4d6/`
(explicit TCG). Existing user machines and pools were preserved.

The separate remote Linux/KVM run on `lanst.net.krglv.com` passed the full
managed Alpine 3.24.3 x86_64 lifecycle in 202 seconds with
`type="q35" acceleration="kvm"`. Its authoritative canonical target was
`pc-q35-rhel8.6.0`, emulator `/usr/libexec/qemu-kvm`. Artifacts remain under
`result/vmango-acceptance-e33f3ac5-28ed-47e0-b3aa-bbdbd664721a/`. The original
eight domains and two pools retained their states; the test VM, pool and remote
pool directory were removed. This run used the existing default CPU overcommit
of four, with 24 configured vCPUs within the 48-CPU admission budget. An earlier
negative-only run had an invalid capacity-rejection precondition and timed out;
its owned resources were also verified cleaned. No package build/install result
or rendered-browser verification is implied by these lifecycle tests.

## Guest system and clock verification

Set `ACCEPTANCE_GUEST_SYSTEM=1` to add explicit clock policy to the disposable
profiles. `ACCEPTANCE_CLOCK_MODE` accepts `utc`, `localtime` or `timezone`
(default: `timezone`, using `Europe/Berlin`). On x86_64 this also configures all
supported descriptive SMBIOS fields, RTC/PIT catchup, HPET absent and KVM/Hyper-V
clocks present; use an eligible Q35/KVM host. The guest must verify eleven SMBIOS
text fields from DMI sysfs and print its SMBIOS UUID matching the domain UUID
before the normal boot marker. BIOS release is checked in the configured XML;
it is not asserted through guest sysfs. Both architectures record the guest RTC
and current clocksource in `console.log`. Resource changes and process restart
must preserve the system/clock XML. The normal isolated cleanup applies.

On ARM/HVF the mode configures only the clock, not SMBIOS or x86 timers. This is
not a test of ARM virtual timer control: `armvtimer` requires an eligible ARM/KVM
host and verified backend support. No available ARM/KVM host was tested in the
recorded run below. Unit tests for its capability gate are not guest evidence.

### Recorded guest-system verification, 2026-11-08

macOS ARM64, libvirt daemon 11.2.0, QEMU 9.2.3, `virt-9.2`/HVF and the cached
Ubuntu 24.04 ARM64 image passed explicit localtime clock lifecycle in 43 seconds.
The guest reported `arch_sys_counter` and an RTC reading; resource changes,
process restart, deletion and cleanup passed. Artifacts:
`result/vmango-acceptance-a086648b-0088-4c94-b9bd-4e6544bf82d8/`.

The Linux Q35/KVM full lifecycle passed in 217 seconds with libvirt daemon
8.0.0 and QEMU 6.2.0 on
`lanst.net.krglv.com`, canonical `pc-q35-rhel8.6.0`, and the pinned Alpine 3.24.3
x86_64 image. Guest DMI fields and UUID were verified; the guest reported
`kvm-clock` with the explicit Europe/Berlin hardware-clock policy and configured
timer combination. Artifacts:
`result/vmango-acceptance-95dc0ad8-2569-4e0d-8386-ac9faf055e6a/`.
A final explicit UTC/EFI HVF run also passed in 43 seconds after the adoption
preflight refinement, under
`result/vmango-acceptance-d7b437b2-ef59-4dd4-a00c-f64d69dfb03d/`.
These are functional boot/preservation observations, not clock-accuracy
measurements or claims about every machine, firmware or timer combination.

## CPU, topology and native placement

Set `ACCEPTANCE_CPU_POLICY=1` to verify CPU policy on real guests. On x86/KVM the
suite requests host-model, requires AES, disables AVX2, enables a small explicit
Hyper-V custom set and KVM poll-control/pv-ipi. Guest cloud-init verifies its
2-vCPU count and feature presence/absence and records `/proc/cpuinfo`. On
macOS/HVF it uses maximum and checks guest CPU count. Every lifecycle run now
checks a stopped resize from one socket/two cores/one thread to one socket/four
cores/one thread, preserving all other CPU and presentation policy. The isolated
acceptance host uses CPU overcommit 8 with full-vCPU/physical-core accounting.

`ACCEPTANCE_NATIVE_AUTO=1` additionally requires a separately prepared Linux/KVM
host with installed/running numad. The suite does not start numad or change host
configuration. Inspect the isolated VM's live `virsh numatune --live` and
`virsh vcpupin --live` results when it is running. A returned strict nodeset is
native libvirt placement, not proof of single-node guarantees, exclusive CPUs or
SMT isolation. Do not change existing guest affinity or host SMT for this test.

### Recorded CPU verification, 2026-11-08

- Local macOS/aarch64 HVF: full lifecycle and actual guest CPU count passed in
  42 seconds; simple stopped topology resize and settings preservation passed.
  Libvirt lacks the online-CPU-map RPC here; active native CPU counts matched the
  complete per-core capabilities topology.
- Remote Linux/x86_64 Q35/KVM, libvirt 8.0.0/QEMU 6.2.0: host-model AES/AVX2
  requirements, custom Hyper-V/KVM flags, native auto placement, guest boot,
  stopped resize, process restart and cleanup passed in 232 seconds. Live memory
  policy was strict on nodeset 0; vCPUs had affinity 0–11 on the 6-core/12-thread
  host. Temporarily started numad was restored to its original inactive state.
- Read-only preflight rejected unknown required/disabled feature names, missing
  models, Hyper-V host-model on libvirt 8, and missing active numad before VM
  provisioning. A first numeric-HCL decoder failure was fixed and regressed
  before the successful Linux run; the failed run created no VM.

These runs do not establish Windows guest behavior, Hyper-V automatic modes on
libvirt >=11.9, other CPU architectures, performance or SMT isolation. Browser
inspection was unavailable because the built-in browser provider was unavailable;
no external Chrome session was used. No package builds were performed.

## Firmware and NVRAM

Set `ACCEPTANCE_FIRMWARE=efi` for a real guest-written UEFI variable, private
NVRAM path separation, external adoption ownership, persistence through resource
changes and service restart, missing-file regeneration after a catalog change,
and missing-owned-file cleanup. All removal targets come from disposable test
VMs; shared CODE/templates are only read and hashed.

`ACCEPTANCE_FIRMWARE=secure` additionally asserts the guest SecureBoot variable
is enabled. Use a signed guest trusted by the selected template (for example a
checksum-verified Ubuntu cloud image); unsigned Alpine is unsuitable. Select a
compatible Q35/KVM host explicitly. Report ordinary UEFI, Secure Boot, raw/qcow2,
Linux/KVM and macOS/HVF separately. This mode does not claim every firmware build
or certificate set works. See [firmware policy](../docs/FIRMWARE.md).

To verify a named artifact pair, additionally set `ACCEPTANCE_FIRMWARE_LOADER`
and `ACCEPTANCE_FIRMWARE_TEMPLATE` to absolute hypervisor paths and
`ACCEPTANCE_FIRMWARE_FORMAT=raw` or `qcow2`. Both artifacts must already have
that format. The suite treats them as shared inputs, checks they survive, and
never converts or removes them. Use disposable copies for custom-format checks.

### Recorded firmware verification, 2026-11-08

- Linux x86_64/Q35/KVM, libvirt 8 and QEMU 6.2: the complete Secure Boot
  lifecycle passed with raw CODE/template/private VARS and the verified Ubuntu
  Noble 20261017 amd64 image. The guest reported SecureBoot=1, wrote a persistent
  variable, and the suite verified private-file separation, resource/disk update
  preservation, application restart, external adoption retention, regeneration
  from the saved template after catalog edits, missing-owned-file deletion and
  shared-template preservation. Task history contained the actual NVRAM paths.
  The run also passed the generic raw/qcow2 disk-cloning checks and cleanup.
- macOS aarch64/HVF, libvirt 11.2 and QEMU 9.2.3: after the authorized
  restart of only the stuck virtqemud process, the complete raw EFI lifecycle
  passed (39 seconds). Existing guest PIDs/UUIDs remained unchanged. A separate
  run with disposable named qcow2 CODE/template copies also passed (35 seconds),
  including guest NVRAM persistence, private-file separation, restart, external
  adoption, regeneration from the saved template, deletion and shared-input
  preservation. Both runs used the verified Ubuntu Noble ARM64 image and
  cleaned their own VMs/pools. Linux libvirt 8 cannot satisfy the qcow2 firmware
  version requirements.
- Full Go tests, generated API checks, 206 UI tests, five asset tests and the
  Hugo documentation build passed. The built-in browser provider was unavailable;
  no Chrome session or package build was used.

Firmware mode skips the unrelated storage cloning stress test; the ordinary
suite continues to cover it.

## Disk/controller topology matrix

`ACCEPTANCE_DISK_BUS=virtio|scsi|ide|sata` selects ordinary disks.
SCSI additionally requires `ACCEPTANCE_SCSI_MODEL` (default `virtio-scsi`).
Config drive selection remains independent through
`ACCEPTANCE_CONFIG_DRIVE_BUS`, `ACCEPTANCE_CONFIG_DRIVE_CONTROLLER`,
`ACCEPTANCE_CONFIG_DRIVE_DEVICE=disk|cdrom` and
`ACCEPTANCE_INITIALIZATION=nocloud|openstack`.

Set `ACCEPTANCE_STORAGE_TOPOLOGY=1` for a focused real-hypervisor test. It
boots the guest from the selected root disk, checks config-drive discovery,
rejects active disk changes, powers off, attaches/detaches/reattaches a data
disk, and verifies stable existing XML targets/addresses/controllers and reuse
of the freed address. It then cold-starts and requires the guest to discover
the new disk. The SATA case additionally fills the six-port controller and
checks that overflow creates neither a volume nor a domain change. Each run
owns its random VM/pool; cleanup never targets preexisting workloads.

Example on an explicitly authorized Linux test host:

```sh
ACCEPTANCE_REMOTE=1 ACCEPTANCE_ARCH=x86_64 \
ACCEPTANCE_URI=qemu+ssh://root@HOST/system \
ACCEPTANCE_MACHINE_TYPE=q35 ACCEPTANCE_STORAGE_TOPOLOGY=1 \
ACCEPTANCE_DISK_BUS=sata ACCEPTANCE_CONFIG_DRIVE_BUS=sata \
ACCEPTANCE_CONFIG_DRIVE_DEVICE=cdrom \
ACCEPTANCE_IMAGE_URL=URL ACCEPTANCE_IMAGE_SHA256=SHA256 \
ACCEPTANCE_IMAGE_SIZE_BYTES=BYTES make acceptance
```

The initial plan and later capacity boundaries also have focused Go regressions:
SATA six ports (five ordinary disks including root when config drive shares
SATA), IDE four positions, LSI reserved ID 7, SAS/PVSCSI model-specific address
spaces, separate and shared SCSI models, multiple existing controllers,
exhausted PCI slots, unsupported observed models, no provisioning on preflight
failure, and namespace forwarding of the complete attachment set.

### Recorded storage verification, 2026-11-08

Linux x86_64/KVM uses libvirt 8/QEMU 6.2 and the checksum-verified Alpine 3.24.3
cloud image from the command earlier in this guide. Results are specific to
these platform/image combinations, not universal guest-driver claims.

| Ordinary disks | Independent config drive | Platform | Result |
| --- | --- | --- | --- |
| SATA/AHCI | SATA CD-ROM, NoCloud | Q35/KVM | Full lifecycle, topology/data discovery and six-port overflow with zero effects passed |
| virtio-blk | virtio disk, NoCloud | Q35/KVM | Focused topology/data discovery passed |
| virtio-scsi | virtio-scsi disk, OpenStack | Q35/KVM | Focused topology/data discovery passed |
| IDE | IDE CD-ROM, NoCloud | i440fx/KVM | Focused topology/data discovery passed |
| lsilogic | virtio-scsi CD-ROM, OpenStack | i440fx/KVM | Host QEMU lacks LSI53C895A; rejected before target VM/artifact creation |
| lsisas1068 | lsisas1068 disk, NoCloud | Q35/KVM | Host QEMU lacks SAS1068; rejected before target VM/artifact creation |
| vmpvscsi | virtio-scsi CD-ROM, NoCloud | Q35/KVM | Host QEMU lacks PVSCSI; rejected before target VM/artifact creation |

On macOS ARM64/HVF, newer QEMU accepts LSI controller XML, but the installed
EDK2 firmware did not expose that root disk and entered its shell; this is not
a successful LSI guest lifecycle. Emulated SCSI positive guest coverage remains
unverified on available platform/image combinations.

Full Go tests, generated API checks, 207 UI tests, five asset checks and Hugo
build passed. The built-in browser provider was unavailable, so rendered-page
verification could not be performed; Chrome was not used. No remote package
build or publication is included in these results. See [disk policy](../docs/DISKS.md)
for the supported allocation subset and official device references.

## Graphics and USB input policies

Set `ACCEPTANCE_DISPLAY=headless`, `vnc`, or `vmvga` with a compatible cloud
image. `vnc` selects virtio video, a USB keyboard and tablet; `vmvga` selects
VMware SVGA and a USB keyboard and mouse. Both share one qemu-xhci controller.
`headless` selects no graphics/video/managed USB devices and proves an actual
root serial login plus a shell command using a random test-only password.
Serial service and cloud-init must be enabled in the image. The graphical tests
use Python 3 and Linux evdev inside the disposable guest to confirm keyboard
and pointer events delivered through the authenticated VNC WebSocket. They
capture a real framebuffer in `framebuffer.ppm`; protocol checks do not replace
a rendered browser check. Each mode also checks topology after libvirt
normalization, resource changes and application restart, and runs the complete
lifecycle with cleanup.

```sh
ACCEPTANCE_DISPLAY=headless ACCEPTANCE_DISK_BUS=virtio \
ACCEPTANCE_IMAGE="$PWD/images/noble-server-cloudimg-arm64.img" make acceptance
# Repeat with ACCEPTANCE_DISPLAY=vnc on the same compatible ARM image.
```

For an authorized remote Linux host, combine the remote command above with
`ACCEPTANCE_DISPLAY=vnc` (or `headless`/`vmvga`), `ACCEPTANCE_DISK_BUS=virtio`
and `ACCEPTANCE_NETWORK=default` to bind an existing native libvirt network.
This does not create or change that network. Native networking avoids raw-QEMU
PCI devices whose transport capacity cannot be safely inferred. Run modes
serially per host; every run owns a distinct temporary VM and storage pool.

### Recorded graphics verification, 2026-11-08

The new parser/default/dependency/adoption/drift regressions, full Go suite,
209 UI tests, five asset checks, generated API checks and Hugo build passed.
The built-in browser provider was unavailable, so rendered-page and interactive
noVNC browser checks remain unverified; Chrome was not used. No remote package
build or publication is included in these results.

| Platform and guest | Mode | Result |
| --- | --- | --- |
| macOS ARM64/HVF, Ubuntu Noble cloud image | Headless | Full lifecycle, no graphics/video/managed USB, serial root login and shell command passed |
| macOS ARM64/HVF, Ubuntu Noble cloud image | VNC/virtio + USB keyboard/tablet | Full lifecycle, visible guest framebuffer, guest-observed RFB keyboard/pointer events, stable topology after resource change/restart passed |
| Linux x86_64 Q35/KVM, Alpine 3.24.3 cloud image | Headless | Full lifecycle and actual serial root login/shell passed |
| Linux x86_64 Q35/KVM, Alpine 3.24.3 cloud image | VNC/virtio + USB keyboard/tablet | Full lifecycle, visible guest framebuffer and guest-observed keyboard/pointer events passed |

These are device/driver observations for the listed combinations, not universal
compatibility claims. Initial harness retries exposed missing RFB encoding
negotiation and serial-getty readiness handling; the retained test implements
both, with bounded input/login waits. A nested connection lease in new metadata
recording was also corrected before the successful runs. Each finished run
cleaned only its owned temporary resources; existing user VMs remained running.

Linux QEMU 6.2 on the tested host omits `vmware-svga`; `vmvga` was rejected
with HTTP 422 before target VM provisioning. This is a negative capability
check, not a successful vmvga guest test.

Local x86 Q35/software emulation exposes vmware-svga, but a full positive guest
check was unavailable: raw `qemu-usernet` devices collided with a libvirt-assigned
video PCI slot, while native user-network preflight required unsupported macOS
TAP allocation. That networking implementation was not changed in this feature. New x86
video/raw-network combinations now fail actionable preflight before effects;
legacy plans, ARM MMIO and native-network cases remain unchanged.
No positive vmvga framebuffer/guest-input result is claimed. Use a compatible
x86 Linux QEMU build and native network for this verification. The harness also
accepts `ACCEPTANCE_ARCH=x86_64 ACCEPTANCE_VIRT_TYPE=qemu` for explicit local
software-emulation tests; those never count as HVF/KVM results.
