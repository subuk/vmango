---
title: "Guest CPU policies"
weight: 45
description: "CPU presentation, physical-core admission, native placement and safe resource changes."
---

Administrators declare CPU and hypervisor presentation in the complete hardware
profile selected by an image. Users select an image and an instance type. The
instance type's `cpu` is the only total vCPU quantity; profiles cannot declare
sockets, cores, threads, dies, clusters, current/max vCPUs or guest NUMA cells.
There is no profile inheritance or per-image CPU override.

## CPU presentation

```hcl
hardware_profile "server" {
    cpu {
        mode = "host-model"
        feature "aes" { policy = "require" }
        feature "avx2" { policy = "disable" }
    }
}

hardware_profile "intel-compatible" {
    cpu {
        mode = "custom"
        model = "Skylake-Server-v4"
        vendor = "Intel"
    }
}
```

An omitted block/mode defaults to `maximum` for new guests. `maximum`,
`host-passthrough`, `host-model` and `custom` must be supported by the exact
architecture, machine, acceleration and emulator. `custom` requires a backend
model and emits an exact match with `fallback="forbid"`. Other modes forbid
`model` and `vendor`. A custom vendor is a compatibility requirement, such as
`Intel` or `AMD`, not vendor-ID spoofing. The example model is illustrative;
Vmango obtains usable models from domain capabilities instead of an allowlist.

Feature names are libvirt CPU feature names. Policies are `require` (the default)
and `disable`; duplicates, unknown names and incompatible requirements fail.
No features means the mode/model's ordinary feature set. Force, optional,
forbid and arbitrary QEMU properties are unavailable. Exact hypervisor CPU
comparison validates feature/vendor compatibility. If the backend cannot provide
a comparable model, explicit feature support is reported as unverified. In
particular, KVM and macOS/HVF do not have interchangeable feature controls.

Acceptance and execution check the exact backend without defining probe VMs.
Unsupported or unknown requirements exclude the host before disk/config-drive
creation. There is no model substitution, hidden feature repair or acceleration
fallback. Host-derived modes can change their effective capabilities after a
host/software change or cold start; these controls do not promise a frozen CPU
ABI, live migration, physical pinning or NUMA placement.

## Physical-core admission and migration

Host vCPU budget is:

```text
(online physical cores - cpu_reserve) * cpu_overcommit
```

`cpu_overcommit` defaults to 4 and must be at least 1. `cpu_reserve` defaults to
zero and counts physical cores, not logical CPUs. Reliable online topology uses
package/die/cluster/core identities; missing information prevents admission with
a diagnostic. Local macOS uses native active CPU counts cross-checked against
libvirt topology when its online-map RPC is unavailable; ambiguous topology
still fails. SMT threads never inflate the budget. For example, 16 physical
cores, reserve 2 and factor 4 allow 56 configured vCPUs with SMT enabled or disabled.

Every VM's full configured vCPU count consumes capacity, including stopped and
unmanaged VMs. An 8-vCPU VM with historical `CPURequest=2` consumes eight units.
Instantaneous utilization does not affect admission. Create and stopped resize
use the same accounting under the existing serialized capacity commands. If
accounting reveals an overcommitted host, existing VMs remain intact. Additional
consumption of an exhausted resource fails; compatible reductions and RAM-only
changes that do not grow exhausted CPU occupancy remain possible. Memory
quantities/reserves retain their previous non-overcommitted meaning.

Remove `cpu_request` from configuration. It now causes an actionable startup
error and is absent from API/catalog/Administration fields. Historical VM
allocation metadata remains readable and is never used to discount occupancy.
Upgrade changes command version 3 to 4: pending pre-change commands fail at the
existing compatibility boundary and must be inspected/resubmitted. Completed
history and effects remain; uncertain started work retains its resource fences.
There is no automatic replay. Existing version-1 applied hardware policies stay
supported without adding absent optional CPU settings or rewriting hardware.

Reserves are budget deductions, not reserved CPU IDs. Overcommit is admission
accounting, not a runtime quota, per-core sharing limit, dedicated CPU guarantee
or promise of a fixed CPU fraction.

## New topology and stopped resize

New VMs use one socket, `instance_type.cpu` cores and one thread per core. An
8-vCPU VM emits `sockets="1" cores="8" threads="1"`; 16 vCPUs change only cores.
There is no guest NUMA split. Platform topology/vCPU limits are checked without
rounding or copying host SMT topology. Unified guest memory may span host NUMA
nodes; large guests are not guaranteed optimal locality.

Instance-type changes require a stopped VM; CPU hotplug is unavailable. Simple
topology changes update the core count while retaining CPU/model/features,
Hyper-V/KVM, placement, autostart, prepared hugepages and other devices. A vCPU
change that would restructure a non-simple topology, guest NUMA or established
affinity fails before mutation. Compatible RAM-only changes preserve those
settings. Vmango does not calculate, redistribute or remove pins.

Adoption requires an explicitly selected compatible profile and preserves
existing XML. Omitted mode/placement settings are not authorization to reset
existing hardware to new-VM defaults. Explicit constraints must match; custom
Hyper-V settings must match the selected set. Existing extra backend presentation
is observable and retained when unconstrained. Applied policy is stored with the
VM independently of task history. Profile catalog edits affect new work, not
existing VMs or supported accepted plans. There is no CPU-model/topology or
hypervisor-feature reconfiguration operation for existing VMs.

## Native placement

```hcl
hardware_profile "numa-auto" {
    cpu { mode = "maximum" }
    placement {
        mode = "auto"
        memory_mode = "strict"
    }
}
```

No placement block, an empty block, or `mode="static"` means ordinary host
scheduling without new cpusets/pins or NUMA memory restrictions. Static does not
mean dedicated CPUs. Under `auto`, `memory_mode` defaults to the only accepted
value, `strict`; it is invalid under static. Vmango emits `vcpu placement="auto"`
and `numatune memory placement="auto" mode="strict"`.

Auto requires Linux KVM, NUMA support and installed/running `numad`. A bounded
read-only prerequisite check uses the configured local/SSH connection; other
transports fail with an unverified-prerequisite diagnostic. Vmango never starts
numad or changes host services, SMT, isolation or `qemu.conf`. The exact driver's
native command validation must also accept the requested policy.

Libvirt/numad selects an advisory nodeset that may contain multiple nodes. Strict
memory placement constrains allocation to that set; it does not guarantee a
single node, frozen assignment, exclusive cores or aligned guest/host SMT.
Existing explicit affinity cannot be discarded to adopt auto placement.
Administration distinguishes requested, applied, persistent observed and live
CPU/placement state; observed live values are libvirt XML, not a benchmark.

## Hyper-V and KVM presentation

No `hyperv` block enables no enlightenments for new VMs. OS/image names never
select a preset. A present block defaults to `custom`. Modes `host-model`
(libvirt >= 11.9.0) and `passthrough` delegate the supported set to the backend and
forbid all feature overrides. Automatic sets can include features outside the
custom contract and change across hosts/software versions. Unsupported modes
fail. These are guest interfaces, not installation of Microsoft Hyper-V.

This complete custom example includes the shared Hyper-V clock timer:

```hcl
hardware_profile "windows" {
    cpu { mode = "maximum" }
    timer "hypervclock" { present = true }
    hyperv {
        mode = "custom"
        relaxed = true
        vapic = true
        vpindex = true
        runtime = true
        synic = true
        reset = true
        frequencies = true
        tlbflush = true
        ipi = true
        spinlocks { enabled = true retries = 4096 }
        stimer { enabled = true direct = false }
    }
    kvm { hidden = false poll_control = true pv_ipi = true }
}
```

This is not a universal preset: verify actual support. Unspecified custom
Hyper-V features are disabled. Absent spinlocks/stimer blocks are disabled;
present blocks default enabled. Enabled spinlocks always specify retries,
default 4096, range 4095..4294967295 (the last value means never notify).
Backend/version validation still applies. Disabled features reject parameters
that would be ignored. `direct=true` requires enabled stimer. Synic, tlbflush
and ipi require explicit vpindex; stimer also requires synic and explicit
`hypervclock present=true`. Dependencies are never enabled automatically.
Automatic modes conflict with explicitly disabled hypervclock. There is no
second Hyper-V time control, nested-virtualization workflow, custom evmcs,
reenlightenment, vendor-ID or advanced tlbflush controls.

KVM's optional booleans map to `hidden`, `poll-control`, `pv-ipi`. New VMs do not
hide KVM by default. Omitted poll-control/pv-ipi retain backend defaults; explicit
false remains a constraint. Hidden affects compatibility discovery, not a claim
that virtualization is undetectable. Dedicated-CPU hints and dirty-ring are not
configurable here.

## Isolation and verification

Guest topology, native placement and ordinary affinity do not isolate mutually
untrusted VMs on sibling SMT threads. Linux core scheduling requires
`CONFIG_SCHED_CORE`, a supported kernel and a deliberate trust-group policy;
libvirt's `qemu.conf` `sched_core` integration must be prepared by the operator.
Its limitations still apply. Vmango does not implement a replacement scheduler
or claim SMT isolation. KRG-42 owns future explicit NUMA-local allocations,
physical-core ownership, SMT policy, generated pins and aligned topology.

Use the opt-in acceptance suite's `ACCEPTANCE_CPU_POLICY=1` to verify real guest
CPU count/features, topology and stopped resize preservation. On an explicitly
prepared Linux host add `ACCEPTANCE_NATIVE_AUTO=1`; inspect live placement and
numad behavior as well as XML. Report Linux/KVM, macOS/HVF, architecture-specific
and Windows guest results separately. A Linux guest boot does not establish
Windows behavior, single-node placement, clock accuracy or SMT isolation.

References: [libvirt CPU configuration](https://libvirt.org/formatdomain.html#cpu-model-and-topology),
[domain capabilities](https://libvirt.org/formatdomaincaps.html#cpu-configuration),
[NUMA tuning](https://libvirt.org/formatdomain.html#numa-node-tuning),
[QEMU Hyper-V interfaces](https://www.qemu.org/docs/master/system/i386/hyperv.html),
[Linux core scheduling](https://www.kernel.org/doc/html/latest/admin-guide/hw-vuln/core-scheduling.html).
