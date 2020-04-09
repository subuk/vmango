package compute

type DeviceBus int

const (
	DeviceBusUnknown DeviceBus = iota
	DeviceBusVirtio
	DeviceBusIde
	DeviceBusScsi
)

func (DeviceBus DeviceBus) String() string {
	switch DeviceBus {
	default:
		return "unknown"
	case DeviceBusVirtio:
		return "virtio"
	case DeviceBusIde:
		return "ide"
	case DeviceBusScsi:
		return "scsi"
	}
}

func NewDeviceBus(input string) DeviceBus {
	switch input {
	default:
		return DeviceBusUnknown
	case "virtio":
		return DeviceBusVirtio
	case "ide":
		return DeviceBusIde
	case "scsi":
		return DeviceBusScsi
	}
}
