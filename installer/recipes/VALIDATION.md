# Installer recipe validation

Validation on 2026-11-07 used Richt source revision
`43f24f90aae04766f8c5979b06ce08cb33e703eb` and immutable recipe source commit
`6b6b25708b018f6297cd0ca591e0eaf967dc35d2`.

| Controller and target | Verified scope | Result |
| --- | --- | --- |
| macOS arm64 controller → two independent AlmaLinux 10.2 x86_64 SSH targets | Fresh facts; strict existing host-key trust; explicit non-root `sudo -n`; pinned HTTPS recipes; packages, group membership, Polkit rule, directories, modular libvirt sockets | 50 initial tasks succeeded; fresh reconciliation produced zero tasks on both hosts |
| AlmaLinux 10.2 x86_64 local controller/target | Fresh facts, pool free-space observation, local recipe preparation and application after the SSH installation | Exact zero-task plan and successful no-op application |
| Ubuntu 24.04.5 x86_64 systemd container on the AlmaLinux test host | Native local APT, files, group and `libvirtd.socket` recipe execution | 14 initial tasks succeeded; fresh reconciliation produced zero tasks |

The AlmaLinux targets expose no KVM device. Their stock
`qemu-kvm-core-10.1.0-16.el10_2.5.alma.1.x86_64` executable supports TCG.
System libvirt accessed as the non-root runtime account reports default domain
type `qemu` and CPU mode `maximum` supported. These observations establish
software-emulation availability, not KVM execution coverage. VM lifecycle
validation is recorded separately from recipe validation.

Existing unrelated services remained active, original supplementary account
memberships were preserved, SELinux remained Enforcing, and no failed host
systemd units remained. The generated inventory never requests installation or
restart of the Vmango server.

The Ubuntu container used the official base archive
`ubuntu-base-24.04.5-base-amd64.tar.gz`, verified against the official HTTPS
SHA-256 manifest:

```text
e77b6f10c2590cef872b33ee9f635a0e3fd1f57fb074c0e52b5c7f56147a0c86
```

It ran systemd 255, Python 3.12.3, QEMU 8.2.2 and libvirt
`10.0.0-2ubuntu8.19`. Package repository signature verification remained
enabled. `CAP_NET_ADMIN` and `CAP_NET_RAW` were removed from the container's
capability bounding set, preventing changes to the host's shared network
namespace. The container and its transient unit were removed after private
logs and plans were preserved. This is Ubuntu userspace/systemd coverage; it
does not verify Ubuntu guest networking, VM lifecycle or a separate Ubuntu
kernel. Bootstrap used `--timezone=off` to avoid a host timezone bind mount
conflicting with the container's `tzdata` package configuration.

Additional checks:

- Focused adapter tests passed with the Go race detector, including executable
  and plan replacement, changed versus preserved plan progress, cancellation,
  bounded process output, exact saved-plan invocation and partial outcomes.
- Generated recipe identifiers and each source YAML passed validation.
- Storage inspection reported free space on existing ancestors of missing
  paths without creating directories; symlink and non-directory ancestry were
  rejected in local tests and live SSH checks.
- The configuration installer tests passed on Linux as an ordinary non-root
  user, covering preservation, validation, backups, conflicts and publication.

The combined Linux-local-plus-SSH topology, Ubuntu arm64, RHEL proper, Rocky
Linux and CentOS Stream have not been exercised by these runs. Matching recipe
selection is not a claim of live validation for those combinations.

## Wizard and VM integration

The final macOS arm64 application completed all three wizard stages against
both AlmaLinux SSH hosts: operating-system preparation, isolated persistent
NAT/directory-pool creation, and native configuration publication. An existing
configuration was then inspected and passed through the wizard again. Existing
resource discovery returned the real subnet, DHCP range and pool paths; all
three stages reconciled without changes. The configuration bytes, administrator
hash and key/state paths were unchanged, with no additional backup.

The normal Vmango server was started separately with that exact verified
configuration. `scripts/test-installer-lifecycle.py` passed authentication,
VM creation, boot disk and config-drive creation, startup, VNC WebSocket RFB
handshake, serial console, cloud-init, hostname, root disk, DHCP IPv4, default
route, gateway reachability, and VM/attached-disk deletion. Captured live XML
reported domain type `qemu`, CPU mode `maximum`, and machine
`pc-i440fx-rhel10.0.0`; no KVM device or acceleration feature was added.

Each disposable host has approximately 1 GiB RAM. The existing test fixture
therefore used one vCPU, 512 MiB guest RAM, an 8 GiB boot disk, and a matching
512 MiB image minimum. Temporary test-only host swap protected host memory
during the run. Production first-run defaults remain two vCPUs and 2048 MiB.
The x86 catalog uses the existing `auto` firmware behavior: AlmaLinux's default
i440fx machine can boot its installed SeaBIOS but does not advertise an explicit
`bios` firmware selection. AArch64 retains its explicit EFI setting.

After acceptance, the test networks and pools were removed only after UUID
verification and confirmation that no domains remained. The known cached image,
empty pool directories and temporary swap were removed. Existing PostgreSQL
and exporter services remained active, SELinux stayed Enforcing, and both hosts
had no failed systemd units.

Local verification passed `make`, `make test`, race tests for the installer,
configuration, compute, libvirt and bootstrap packages, 146 UI tests, API type
checks, five asset-manifest tests and 39 release-tool tests. The source archive
included the installer and verified with `UI_PREBUILT=1`. Browser inspection
covered the form, an active operation, closing/reopening the page through a
deep link, and the completed state with further edits disabled. Remote RPM/DEB
package builds and hardware KVM execution were not part of this validation.
