package compute

type VolumeFormat int

const (
	VolumeFormatUnknown = VolumeFormat(0)
	VolumeFormatRaw     = VolumeFormat(1)
	VolumeFormatQcow2   = VolumeFormat(2)
	VolumeFormatIso     = VolumeFormat(3)
)

func (format VolumeFormat) String() string {
	switch format {
	default:
		return "unknown"
	case VolumeFormatRaw:
		return "raw"
	case VolumeFormatQcow2:
		return "qcow2"
	case VolumeFormatIso:
		return "iso"
	}
}

func NewVolumeFormat(input string) VolumeFormat {
	switch input {
	default:
		return VolumeFormatUnknown
	case "raw":
		return VolumeFormatRaw
	case "qcow2":
		return VolumeFormatQcow2
	case "iso":
		return VolumeFormatIso
	}
}
