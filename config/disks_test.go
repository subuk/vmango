package config

import (
	"strings"
	"testing"
)

func TestDiskPolicyConfiguration(t *testing.T) {
	for _, bus := range []string{"virtio", "sata", "ide", "scsi"} {
		models := []string{""}
		if bus == "scsi" {
			models = []string{"virtio-scsi", "lsilogic", "lsisas1068", "vmpvscsi"}
		}
		for _, model := range models {
			t.Run(bus+model, func(t *testing.T) {
				fields := `bus="` + bus + `"`
				if model != "" {
					fields += ` controller_model="` + model + `"`
				}
				cfg, err := parseCloudTest(t, withImages(`hardware_profile "storage" {disks {`+fields+`} config_drive {`+fields+` device_type="disk"}} image "test" {path="/base" hardware_profile="storage" os_arch="x86_64"}`))
				if err != nil {
					t.Fatal(err)
				}
				profiles, err := cfg.HardwareProfiles()
				if err != nil {
					t.Fatal(err)
				}
				p := profiles["test"]["storage"]
				if p.Disks == nil || p.Disks.Bus != bus || p.Disks.ControllerModel != model || p.ConfigDrive.ControllerModel != model {
					t.Fatalf("lost policy: %+v", p)
				}
			})
		}
	}
}

func TestDiskPolicyDefaultsAndErrors(t *testing.T) {
	for _, body := range []string{"", `disks {}`} {
		cfg, err := parseCloudTest(t, withImages(`hardware_profile "storage" {`+body+`} image "test" { path="/base" hardware_profile="storage" os_arch="x86_64" }`))
		if err != nil {
			t.Fatal(err)
		}
		profiles, _ := cfg.HardwareProfiles()
		p := profiles["test"]["storage"]
		if p.Disks == nil || p.Disks.Bus != "virtio" || p.ConfigDrive.Bus != "virtio" {
			t.Fatal(p)
		}
	}
	for _, body := range []string{
		`disk_bus="virtio"`, `disk_controller="virtio-scsi"`,
		`disks {bus="virtio-blk"}`, `disks {bus="virtio-scsi"}`, `disks {bus="usb"}`,
		`disks {bus="scsi"}`, `disks {bus="scsi" controller_model="auto"}`,
		`disks {controller_model="virtio-scsi"}`, `disks {bus="sata" controller_model="ahci"}`,
		`disks {} disks {}`, `disks "named" {}`, `disks="virtio"`,
		`disks {bus="virtio" bus="sata"}`, `disks {unknown=true}`,
		`config_drive {bus="scsi"}`, `config_drive {bus="sata" controller_model="ahci"}`,
		`config_drive {bus="virtio-scsi"}`, `config_drive {device_type="cdrom"}`,
		`config_drive {} config_drive {}`, `config_drive {bus="sata" bus="ide"}`,
	} {
		_, err := parseCloudTest(t, withImages(`hardware_profile "storage" {`+body+`} image "test" { path="/base" hardware_profile="storage" os_arch="x86_64" }`))
		if err == nil {
			t.Errorf("accepted %s", body)
			continue
		}
		if !strings.Contains(err.Error(), "hardware_profile") {
			t.Errorf("missing configuration context: %v", err)
		}
	}
}

func TestDocumentedStorageProfilesParse(t *testing.T) {
	cfg, err := Parse("../docs/examples/storage.conf")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := cfg.HardwareProfiles()
	if err != nil || len(profiles["storage"]) != 7 {
		t.Fatal(profiles, err)
	}
}
