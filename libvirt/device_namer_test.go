package libvirt

import (
	"reflect"
	"subuk/vmango/compute"
	"testing"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

func TestNewDeviceNamerFromDisks(t *testing.T) {
	type args struct {
		disks []libvirtxml.DomainDisk
	}
	diskSetOk := []libvirtxml.DomainDisk{
		libvirtxml.DomainDisk{
			Target: &libvirtxml.DomainDiskTarget{Dev: "vdb"},
		},
		libvirtxml.DomainDisk{
			Target: &libvirtxml.DomainDiskTarget{Dev: "hdc"},
		},
		libvirtxml.DomainDisk{
			Target: &libvirtxml.DomainDiskTarget{Dev: "sda"},
		},
	}
	diskSetOnlyIde := []libvirtxml.DomainDisk{
		libvirtxml.DomainDisk{},
		libvirtxml.DomainDisk{
			Target: &libvirtxml.DomainDiskTarget{Dev: "hdd"},
		},
	}
	diskSetNoDevOrTarget := []libvirtxml.DomainDisk{
		libvirtxml.DomainDisk{},
		libvirtxml.DomainDisk{
			Target: &libvirtxml.DomainDiskTarget{},
		},
	}
	tests := []struct {
		name string
		args args
		want *DeviceNamer
	}{
		{
			name: "ok",
			args: args{disks: diskSetOk},
			want: &DeviceNamer{state: map[compute.DeviceBus]int{
				compute.DeviceBusIde:    3,
				compute.DeviceBusScsi:   1,
				compute.DeviceBusVirtio: 2,
			}},
		},
		{
			name: "only ide",
			args: args{disks: diskSetOnlyIde},
			want: &DeviceNamer{state: map[compute.DeviceBus]int{
				compute.DeviceBusIde: 4,
			}},
		},
		{
			name: "no dev",
			args: args{disks: diskSetNoDevOrTarget},
			want: &DeviceNamer{state: map[compute.DeviceBus]int{}},
		},
		{
			name: "empty disks",
			args: args{disks: nil},
			want: &DeviceNamer{state: map[compute.DeviceBus]int{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewDeviceNamerFromDisks(tt.args.disks); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewDeviceNamerFromDisks() = %v, want %v", got, tt.want)
			}
		})
	}
}
