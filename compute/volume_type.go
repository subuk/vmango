package compute

type VolumeType int

const (
	VolumeTypeUnknown = VolumeType(0)
	VolumeTypeFile    = VolumeType(1)
	VolumeTypeBlock   = VolumeType(2)
)

func (format VolumeType) String() string {
	switch format {
	default:
		return "unknown"
	case VolumeTypeFile:
		return "file"
	case VolumeTypeBlock:
		return "block"
	}
}

func NewVolumeType(input string) VolumeType {
	switch input {
	default:
		return VolumeTypeUnknown
	case "file":
		return VolumeTypeFile
	case "block":
		return VolumeTypeBlock
	}
}
