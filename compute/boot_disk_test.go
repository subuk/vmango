package compute

import "testing"

func TestDefaultBootDisk(t *testing.T) {
	root := &VirtualMachineAttachedVolume{Alias: "root", DeviceType: DeviceTypeDisk}
	data := &VirtualMachineAttachedVolume{Alias: "data", DeviceType: DeviceTypeDisk}
	config := &VirtualMachineAttachedVolume{Alias: "configdrive", DeviceType: DeviceTypeDisk}
	optical := &VirtualMachineAttachedVolume{DeviceType: DeviceTypeCdrom}
	for _, tt := range []struct {
		name  string
		disks []*VirtualMachineAttachedVolume
		want  *VirtualMachineAttachedVolume
	}{
		{"root after config and data", []*VirtualMachineAttachedVolume{config, data, root}, root},
		{"single disk without root alias", []*VirtualMachineAttachedVolume{config, data, optical}, data},
		{"config only", []*VirtualMachineAttachedVolume{config}, nil},
		{"optical only", []*VirtualMachineAttachedVolume{optical}, nil},
		{"ambiguous disks", []*VirtualMachineAttachedVolume{data, {DeviceType: DeviceTypeDisk}}, nil},
		{"duplicate roots", []*VirtualMachineAttachedVolume{root, root}, nil},
		{"empty", []*VirtualMachineAttachedVolume{nil}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultBootDisk(tt.disks); got != tt.want {
				t.Fatalf("selected %v, want %v", got, tt.want)
			}
		})
	}
}
