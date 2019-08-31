package compute

type DeviceType int

const (
	DeviceTypeUnknown = DeviceType(0)
	DeviceTypeDisk    = DeviceType(1)
	DeviceTypeCdrom   = DeviceType(2)
)

func (DeviceType DeviceType) String() string {
	switch DeviceType {
	default:
		return "unknown"
	case DeviceTypeDisk:
		return "disk"
	case DeviceTypeCdrom:
		return "cdrom"
	}
}

func NewDeviceType(input string) DeviceType {
	switch input {
	default:
		return DeviceTypeUnknown
	case "disk":
		return DeviceTypeDisk
	case "cdrom":
		return DeviceTypeCdrom
	}
}
