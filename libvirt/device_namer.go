package libvirt

import (
	"subuk/vmango/compute"

	libvirtxml "libvirt.org/go/libvirtxml"
)

type DeviceNamer struct {
	state map[compute.DeviceBus]int
}

func NewDeviceNamer() *DeviceNamer {
	return &DeviceNamer{
		state: map[compute.DeviceBus]int{},
	}
}

func NewDeviceNamerFromDisks(disks []libvirtxml.DomainDisk) *DeviceNamer {
	namer := &DeviceNamer{state: map[compute.DeviceBus]int{}}
	for _, disk := range disks {
		if disk.Target == nil {
			continue
		}
		for bus, prefix := range map[compute.DeviceBus]string{compute.DeviceBusScsi: "sd", compute.DeviceBusIde: "hd", compute.DeviceBusVirtio: "vd"} {
			if index, ok := diskTargetIndex(disk.Target.Dev, prefix); ok && index+1 > namer.state[bus] {
				namer.state[bus] = index + 1
			}
		}
	}

	return namer
}

func (n *DeviceNamer) Next(bus compute.DeviceBus) string {
	name := ""
	switch bus {
	case compute.DeviceBusIde:
		name = "hd" + diskTargetName(n.state[bus])
	case compute.DeviceBusScsi:
		name = "sd" + diskTargetName(n.state[bus])
	case compute.DeviceBusVirtio:
		name = "vd" + diskTargetName(n.state[bus])
	}
	n.state[bus] += 1
	return name
}
