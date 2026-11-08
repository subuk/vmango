# Firmware profiles and private NVRAM

Firmware is part of the complete libvirt hardware profile selected by an image.
Instance types supply resources; images and individual create requests cannot
override firmware. Configuration belongs to the libvirt cloud and its hosts.

## Selection

```hcl
hardware_profile "platform-default" { firmware { type = "auto" } }
hardware_profile "legacy-bios" { firmware { type = "bios" } }
hardware_profile "ordinary-uefi" { firmware { type = "efi" } }
hardware_profile "secure-uefi" {
    firmware { type = "efi" secure_boot = true }
}
hardware_profile "custom-keys" {
    firmware { type = "efi" secure_boot = true enrolled_keys = false }
}
```

These are fragments inside a cloud. Omitted or empty `firmware {}` means `auto`.
It leaves the platform default unspecified, does not imply UEFI or security,
and never emits `os firmware="auto"`. Explicit `bios` or `efi` requires that
selection to succeed; there is no fallback to another type. A host can provide
implicit platform BIOS while lacking a descriptor for explicit BIOS selection.

| Type | Secure Boot | Initial keys | Result |
| --- | --- | --- | --- |
| auto / bios | omitted | omitted | No UEFI security requirement |
| auto / bios | any explicit value | any explicit value | Rejected |
| efi | omitted / false | omitted / false | Firmware without Secure Boot support; no keys selection filter |
| efi | false | true | Rejected |
| efi | true | omitted / true | Secure Boot-capable firmware and initially enrolled keys |
| efi | true | false | Secure Boot-capable firmware with initially empty keys |

`secure_boot` and `enrolled_keys` correspond to the libvirt firmware features
`secure-boot` and `enrolled-keys`. They describe different properties. With no
initial keys, signature enforcement is not promised. A signed OS must also be
trusted by the particular template's certificates. Vmango does not enroll,
generate, rotate or remove keys. Guest changes to the private UEFI database are
normal and do not change the saved initial selection policy.

Defaults apply to new configurations. Existing supported applied policy and
accepted commands keep their original meaning, including historical firmware
fields that predate this configuration migration.

## Named policy and host bindings

```hcl
cloud "production" {
    firmware_profile "ovmf-release-a" {
        type = "efi"
        secure_boot = true
        enrolled_keys = true
        loader_format = "raw"
        nvram_format = "raw"
    }
    hardware_profile "pinned-server" {
        firmware { profile = "ovmf-release-a" }
    }
    host "kvm-01" {
        uri = "qemu+ssh://root@kvm-01/system"
        firmware_profile "ovmf-release-a" {
            loader = "/srv/firmware/ovmf/release-a/OVMF_CODE.fd"
            nvram_template = "/srv/firmware/ovmf/release-a/OVMF_VARS.fd"
        }
    }
    host "kvm-02" {
        uri = "qemu+ssh://root@kvm-02/system"
        firmware_profile "ovmf-release-a" {
            loader = "/opt/firmware/ovmf/release-a/OVMF_CODE.fd"
            nvram_template = "/opt/firmware/ovmf/release-a/OVMF_VARS.fd"
        }
    }
}
```

The paths are illustrative and refer to each hypervisor, not the Vmango server.
A name or filename does not establish firmware capabilities or key contents.
Install compatible artifacts before using the policy. A missing host binding
makes that candidate ineligible; other compatible hosts may still be selected.
A reference cannot combine `profile` with `type` or local overrides. Duplicate
IDs, unknown references, relative paths and inappropriate fields are rejected.

A named BIOS profile uses `type = "bios"` and a host `loader` ROM path, without
VARS, security or enrolled-key settings. Loader type and read-only semantics
are derived from BIOS/UEFI; arbitrary combinations are not configurable.

## Host evidence and compatibility

Selection uses exact architecture, canonical machine, acceleration and emulator
capabilities. Read-only libvirt XML-to-native conversion checks the actual
backend and selects automatic firmware without defining or starting probe VMs.
Vmango correlates the selected CODE with QEMU firmware descriptors and checks
the declared template, architecture, machine, features and formats. Ambiguous
metadata fails with a request for an explicit named profile.

Firmware metadata and file headers have no libvirt RPC. This implementation
reads them through the configured local or SSH connection, with host-key
checking, bounded output and timeouts. Other transports cannot establish this
evidence and reject these new firmware requests. Vendor descriptors, system
overrides and session-user QEMU overrides are considered. The libvirt native
result remains authoritative; metadata alone cannot select another CODE file.

For custom named artifacts with no matching descriptor, the administrator's
CODE/template contract supplies the missing capability and initial-key evidence.
Known descriptor contradictions, unreadable files, format mismatches and native
backend rejections fail before disks are provisioned. Flags do not modify CODE,
inject keys or prove the contents of custom VARS. Validate custom builds with a
trusted real guest before production use.

Supported UEFI uses read-only pflash CODE and separate persistent file-backed
VARS on x86_64 or aarch64. Secure Boot in this delivery requires x86_64 and a
backend that supports its pflash/SMM requirements. ARM Secure Boot requiring
`uefi-vars`/`varstore`, stateless UEFI, block-backed and network NVRAM are rejected.

Named `loader_format` and `nvram_format` default independently to `raw` and can
be `raw` or `qcow2`. The latter describes both template and private VARS; no
conversion is performed. Automatic selection retains descriptor formats.
Equivalent implicit-raw XML works on older hosts. Qcow2 CODE requires libvirt
9.2 or newer; qcow2 template/private VARS requires 10.10 or newer for explicit
matching `format` and `templateFormat`, plus actual QEMU support. Version gates
are necessary but do not replace native capability validation.

| Policy / binding | Persistent libvirt XML |
| --- | --- |
| Explicit automatic BIOS/EFI requirement | `os firmware="bios"` / `"efi"` during selection |
| Selected BIOS CODE | `loader type="rom" readonly="yes"` |
| Selected UEFI CODE | `loader type="pflash" readonly="yes"`, `secure` capability |
| Secure Boot / initial keys | Firmware feature constraints; saved with applied policy |
| Selected VARS template | `nvram template="..."` |
| Private VARS | Libvirt-chosen per-domain file in `nvram` |
| Qcow2 CODE | `loader format="qcow2"` |
| Qcow2 VARS | `nvram format="qcow2" templateFormat="qcow2"` |

After resolution, concrete loader/template bindings replace automatic selection
in new domain XML. Selection feature filters are retained in policy rather than
re-emitted alongside manual bindings, avoiding delayed auto-selection on older
libvirt versions. The full policy and host paths are retained in accepted plans and VM
metadata independently of task-history retention. Administration shows requested
policy, applied binding and persistent XML observations. XML observations do not
prove live key contents or guest signature enforcement.

## Ownership, lifecycle and missing files

Libvirt chooses its standard connection-specific NVRAM directory and initializes
the file on the hypervisor. System and session connections can use different
locations. Vmango neither copies remote files locally nor accepts per-VM
placement paths. Two VMs get distinct writable NVRAM; CODE and templates remain
shared read-only inputs.

The durable creation plan records allocation intent, VM identity and namespace.
After definition, the actual libvirt path is persisted in VM policy and recorded
as a `libvirt-nvram` task resource. Interrupted work is not replayed. If failure
prevents positive confirmation of the final path, uncertainty/resource fencing
is retained and cleanup preserves unconfirmed state for explicit inspection.
Normal deletion uses the saved owned path and matching domain identity;
external/adopted state and mismatched or unconfirmed artifacts are preserved.
Missing owned files do not prevent idempotent undefinition. Shared CODE/templates
are never passed to deletion.

Start, stop, restart, process restart, CPU/RAM changes and disk operations preserve
firmware XML and private VARS references. If private NVRAM is missing, ordinary
start lets libvirt initialize it from the **saved** template. Guest changes are
lost; this restores initial defaults, not a backup. Keep the template available
throughout the VM lifetime. Missing CODE or an unusable template produces a
backend error without selecting a replacement. Vmango has no special missing-file
guard, recovery engine or reset command.

Explicit adoption validates compatible observed firmware without rewriting it
or claiming external files. Re-adoption retains ownership only for the same
previously proven owned file. Omitted settings do not impose new-VM defaults.
Detectable changes to CODE/template/path/format require explicit compatible
re-adoption before managed operations. No claim is made to detect every
out-of-band byte or guest-key change.

## Firmware builds and maintenance

Saved paths are **not content hashes**. Replacing CODE at a saved pathname can
change the next cold boot; distribution-managed automatic selection therefore
allows package-managed updates. Replacing a template leaves existing private
VARS unchanged, but later initialization/regeneration uses its new contents.
Adding another descriptor or build does not switch an existing VM. Updating
files beneath a running VM is not a supported firmware-update workflow, and a
new CODE build may be incompatible with existing VARS.

For strict pinning, use immutable versioned paths and consistent artifacts on
all host bindings. Install a new build alongside the old one and create a new
named profile for new VMs. Keep old CODE/templates while any VM or accepted task
references them. Mutable `current`/`latest` symlinks do not pin bytes. Vmango does
not distribute firmware, make per-VM CODE copies, manage a firmware repository
or checksum CODE on every start.

There are no API/UI/CLI operations for BIOS↔UEFI migration, CODE switching,
Secure Boot toggling, key management, NVRAM reset, conversion, snapshot/backup/
restore, firmware upgrade/downgrade or rollback. A new image-based VM receives
fresh template state, not another VM's firmware variables.

## Verification

`ACCEPTANCE_FIRMWARE=efi` extends the disposable lifecycle suite with a guest
UEFI variable, private path separation, persistence through unrelated updates
and application restart, external-adoption preservation, regeneration after a
catalog edit, and missing-owned-file cleanup. `ACCEPTANCE_FIRMWARE=secure` also
requires the guest's SecureBoot variable to report enforcement; use a signed
image trusted by the host template. See [acceptance instructions](../acceptance/README.md).
Report Linux/KVM, macOS/HVF, architecture, format and Secure Boot independently.
An HVF pass or XML-only assertion does not establish KVM/Secure Boot support.

References: [libvirt guest firmware XML](https://libvirt.org/formatdomain.html#guest-firmware),
[libvirt Secure Boot](https://libvirt.org/kbase/secureboot.html), and
[QEMU UEFI variable mechanisms](https://www.qemu.org/docs/master/devel/uefi-vars.html).
