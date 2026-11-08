package config

// Image artifacts match the verified quickstart source catalog. The x86 image
// uses the host's default firmware to support its advertised capabilities.
const installerImageCatalog = `
    hardware_profile "linux-default" {
        name = "Linux AUTO"
        firmware { type = "auto" }
        disks { bus = "virtio" }
        nic_model = "virtio"
        guest_agent = false
        config_drive { enabled = true format = "nocloud" device_type = "disk" bus = "virtio" }
    }
    hardware_profile "linux-uefi" {
        name = "Linux EFI"
        firmware { type = "efi" }
        disks { bus = "virtio" }
        nic_model = "virtio"
        guest_agent = false
        config_drive { enabled = true format = "nocloud" device_type = "disk" bus = "virtio" }
    }
    image "ubuntu-24.04-x86_64" {
        hardware_profile = "linux-default"
        name = "Ubuntu Cloud"
        os_name = "Ubuntu"
        os_version = "24.04 / 20261017"
        os_arch = "x86_64"
        url = "https://cloud-images.ubuntu.com/noble/20261017/noble-server-cloudimg-amd64.img"
        sha256 = "04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94"
        source_format = "qcow2"
        source_size_bytes = 621284352
        minimum_memory_mib = 1024
        minimum_disk_gib = 8
    }
    image "ubuntu-24.04-aarch64-uefi" {
        hardware_profile = "linux-uefi"
        name = "Ubuntu Cloud"
        os_name = "Ubuntu"
        os_version = "24.04 / 20261017"
        os_arch = "aarch64"
        url = "https://cloud-images.ubuntu.com/noble/20261017/noble-server-cloudimg-arm64.img"
        sha256 = "50e24e1fb79e89116e939d90aa7738f7af48c442fa1bc2510131e0b39c50348e"
        source_format = "qcow2"
        source_size_bytes = 616522752
        minimum_memory_mib = 1024
        minimum_disk_gib = 8
    }
`
