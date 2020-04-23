package libvirt

import (
	"subuk/vmango/compute"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
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
		if disk.Target != nil && disk.Target.Dev != "" {
			devName := disk.Target.Dev
			switch devName[0 : len(devName)-1] {
			case "sd":
				namer.state[compute.DeviceBusScsi] = int(devName[len(devName)-1]) - int('`')
			case "hd":
				namer.state[compute.DeviceBusIde] = int(devName[len(devName)-1]) - int('`')
			case "vd":
				namer.state[compute.DeviceBusVirtio] = int(devName[len(devName)-1]) - int('`')
			}
		}
	}
	return namer
}

func (n *DeviceNamer) Next(bus compute.DeviceBus) string {
	name := ""
	switch bus {
	case compute.DeviceBusIde:
		name = "hd" + string('a'+n.state[bus])
	case compute.DeviceBusScsi:
		name = "sd" + string('a'+n.state[bus])
	case compute.DeviceBusVirtio:
		name = "vd" + string('a'+n.state[bus])
	}
	n.state[bus] += 1
	return name
}
