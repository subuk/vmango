# Replace the quick-start cloud block; retain its nested images and API settings.
cloud "local" {
    name = "Local cloud"
    # Keep the nested image definitions from vmango.quickstart.conf here.

    zone "local" { name = "Local zone" }
    network "lan" { name = "Private LAN" }
    storage_class "standard" { name = "Standard storage" }

    instance_type "small" {
        name = "Small"
        cpu = 2
        memory_mib = 2048
        boot_disk_gib = 20
    }

    instance_type "medium" {
        name = "Medium"
        cpu = 4
        memory_mib = 4096
        boot_disk_gib = 40
    }

    host "local" {
        uri = "qemu:///system"
        config_drive_pool = "default"
        zone = "local"
        cpu_reserve = 1
        cpu_overcommit = 4
        memory_reserve_mib = 1024
        network "lan" { network = "default" }
        storage_class "standard" { pool = "default" }
    }
}
