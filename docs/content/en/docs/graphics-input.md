---
title: "Graphics, video and input"
weight: 34
---

A named libvirt hardware profile selects a console transport, a video device and
input devices independently. Images reference the complete profile with
`hardware_profile`; there are no image overrides or operating-system presets.

## Two server profiles

For a guest with virtio disk/network drivers, this serial-only server minimizes
unnecessary device/emulation overhead. It does not make a universal performance
claim and does not change disk caching or host tuning:

```hcl
hardware_profile "server-serial" {
    disks { bus = "virtio" }
    nic_model = "virtio"
    graphics { type = "none" }
}
```

It adds no graphical listener, virtual video, USB controller or USB input. The
serial console remains available. Platform-provided PS/2 devices on some x86
machines are not managed USB input and are not removed from existing machines.

For a server with graphical recovery access and serial access retained:

```hcl
hardware_profile "server-vnc" {
    disks { bus = "virtio" }
    nic_model = "virtio"
    graphics { type = "vnc" video_model = "virtio" }
    input { keyboard = "usb" pointer = "usb-tablet" }
}
```

One shared `qemu-xhci` controller is resolved automatically. The image must contain
virtio video, virtio block/network and USB XHCI/HID drivers, and its firmware must
support the selected video device. A VNC listener alone does not prove guest
video compatibility.

The [complete configuration example](https://github.com/subuk/vmango/blob/master/docs/examples/graphics.conf)
contains both image references, a cloud, zone, instance type, network, storage
class and host. Replace its image path and host bindings with your resources;
for ARM use an `aarch64` image and a supported ARM machine/firmware. All seven
profiles in that file are checked with Vmango's actual HCL parser.

Both examples retain these defaults: automatic firmware, hardware acceleration,
maximum CPU mode, UTC clock, no hugepages, enabled guest-agent channel and an
enabled NoCloud config drive attached as a virtio disk. The channel does not
install a guest agent. Prepare guest cloud-init/agent support separately.

## Exact block semantics

Each block is a singleton. Unknown fields, duplicates, empty values and unsupported
combinations fail with a configuration location. No raw XML or arbitrary device
properties are accepted.

| Setting | Values | New VM default |
| --- | --- | --- |
| `graphics.type` | `none`, `vnc`, `spice` | `vnc` |
| `graphics.video_model` | `virtio`, `vmvga`, `qxl`, `cirrus`, `none` | `virtio` when graphics is enabled; otherwise `none` |
| `input.keyboard` | `usb`, `none` | `usb` when graphics is enabled; otherwise `none` |
| `input.pointer` | `usb-tablet`, `usb-mouse`, `none` | `usb-tablet` when graphics is enabled; otherwise `none` |
| `usb_controller.model` | `qemu-xhci` | One shared controller when USB input needs it; otherwise none |

Defaults are identical on x86 and ARM. `hardware_profile "minimal" {}` selects
VNC, virtio video, USB keyboard/tablet and one XHCI. Omitting a field is different
from explicitly setting `none`. A graphics-enabled profile can disable either
input independently. Disabling both removes their dependency but retains any
explicit controller. `video_model="none"` never substitutes another model.

`graphics { type="none" }` conflicts with an explicitly enabled video/input
setting. It accepts explicit input disables and an independent standalone USB
controller:

```hcl
hardware_profile "headless-usb" {
    graphics { type = "none" }
    usb_controller { model = "qemu-xhci" }
}
hardware_profile "vmvga-mouse" {
    graphics { type = "vnc" video_model = "vmvga" }
    input { keyboard = "usb" pointer = "usb-mouse" }
}
hardware_profile "keyboard-only" { input { pointer = "none" } }
hardware_profile "input-disabled" { input { keyboard = "none" pointer = "none" } }
```

An explicit XHCI is reused by input, never duplicated. Managed keyboard and
pointer use stable identities and USB ports 1 and 2 on controller 0 with four
ports. Backend capability validation runs before provisioning. After definition,
Vmango also checks normalized topology so a platform cannot silently add an
unwanted USB controller. No other USB families, PS/2 configuration, input hotplug,
multiple video devices or 3D controls are exposed.

## Platform and guest support

Vmango checks the exact architecture, machine, accelerator and emulator. It
rejects unsupported requirements instead of substituting a device or software
acceleration. New x86 graphical policies cannot combine video with the raw
`qemu-usernet` or `macos-socket-vmnet` bindings: those NICs allocate PCI slots
outside libvirt and can collide with video/root ports. Vmango rejects this
combination before provisioning. Select a native libvirt network/bridge, or
explicitly disable video. ARM MMIO networks and existing applied plans keep
their behavior; existing devices are never relocated. This is a network/device
allocation restriction, not a claim that the video model itself is unavailable. Libvirt XML/native conversion is a host support check, not proof
that guest drivers or firmware work.

Virtio video plus XHCI/HID is suitable for current Linux guests with corresponding
drivers on supported x86 Q35 and ARM virt configurations. `vmvga` represents the
VMware SVGA device and needs compatible x86 platform/firmware and a guest driver
such as Linux `vmwgfx`. QXL and Cirrus depend on the installed QEMU build and guest
drivers; availability differs across distributions and architectures. Do not
assume that an x86 device model works with ARM firmware. Consult the verification
matrix in [acceptance/README.md](https://github.com/subuk/vmango/blob/master/acceptance/README.md)
for tested combinations and limitations.

## Serial and graphical access

Configure the guest bootloader to output to serial, add the appropriate kernel
console argument and enable a serial getty/login service. Typical x86 guests use
`console=ttyS0,115200` and `serial-getty@ttyS0.service`; ARM virt uses
`console=ttyAMA0,115200` and `serial-getty@ttyAMA0.service`. Device names depend on
the actual platform and guest. Use valid guest credentials. Disabling graphics
does not configure the OS or create a serial login.

Open the machine's **Console** and select **Serial**. This path is independent of
graphics, USB and the guest-agent channel. A second serial session is rejected
unless an explicit takeover is requested. Headless machines have no VNC action.

New graphical listeners bind only to host loopback; there is no editable listener
address. Local libvirt connections use local loopback. Remote connections use
verified non-interactive SSH; non-SSH libvirt transports still attempt SSH on
port 22. Configure trusted host keys, credentials and forwarding. A tunnel failure
only prevents the graphical console, not creation or management. Remote browser
VNC requires HTTPS. Existing authentication, signed links and same-origin checks
remain in effect.

The browser provides noVNC and a serial terminal. SPICE is a selectable backend
transport but this delivery adds no browser SPICE client. A compatible external
SPICE client needs an authorized tunnel to the loopback endpoint. This does not
add VeNCrypt/TLS.

## Existing machines and migration

Replace flat profile `graphics="vnc" video_model="virtio"` with
`graphics { type="vnc" video_model="virtio" }`. Old profile syntax is rejected
with a location and [migration guide](https://github.com/subuk/vmango/blob/master/docs/HARDWARE-PROFILES-MIGRATION.md).
To retain old x86 creation without managed USB input, explicitly set
`input { keyboard="none" pointer="none" }`. To retain a listener without video,
explicitly set `graphics.video_model="none"`. Review defaults before accepting
new work. Deprecated image-inline hardware and host `graphics_listen` remain
warned and ignored; they do not reactivate configuration paths.

Existing applied policies and accepted plans preserve their supported historical
interpretation. A restart or catalog edit does not rebuild devices. Explicit
adoption checks explicitly requested models/disables, preserves omitted input
constraints, and records existing listeners/devices/addresses including additional
controllers and PS/2 devices. It neither adds defaults nor removes existing
hardware. Later unrelated operations preserve the applied topology. Unsupported
pre-upgrade commands retain their command-version boundary and are not replayed.

## Mapping to libvirt

Vmango maps transport to [graphics](https://libvirt.org/formatdomain.html#graphical-framebuffers),
model to [video](https://libvirt.org/formatdomain.html#video-devices), keyboard and
pointer to [input](https://libvirt.org/formatdomain.html#input-devices), and the
shared dependency to a USB [controller](https://libvirt.org/formatdomain.html#controllers).
Serial access uses [serial ports](https://libvirt.org/formatdomain.html#serial-port)
and [console devices](https://libvirt.org/formatdomain.html#console). The
[QEMU XHCI guidance](https://www.qemu.org/docs/master/system/devices/usb.html)
explains controller and guest-driver compatibility. These links explain backend
semantics; users configure Vmango profiles rather than writing XML.
