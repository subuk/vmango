package compute

type NetworkType int

const (
	NetworkTypeUnknown = NetworkType(0)
	NetworkTypeBridge  = NetworkType(1)
	NetworkTypeLibvirt = NetworkType(2)
)

func (networkTypw NetworkType) String() string {
	switch networkTypw {
	default:
		return "unknown"
	case NetworkTypeBridge:
		return "bridge"
	case NetworkTypeLibvirt:
		return "libvirt"
	}
}

func NewNetworkType(input string) NetworkType {
	switch input {
	default:
		return NetworkTypeUnknown
	case "bridge":
		return NetworkTypeBridge
	case "libvirt":
		return NetworkTypeLibvirt
	}
}
