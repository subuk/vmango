package compute

type VolumeFormat int

const (
	FormatUnknown = VolumeFormat(0)
	FormatRaw     = VolumeFormat(1)
	FormatQcow2   = VolumeFormat(2)
	FormatIso     = VolumeFormat(3)
)

func (format VolumeFormat) String() string {
	switch format {
	default:
		return "unknown"
	case FormatRaw:
		return "raw"
	case FormatQcow2:
		return "qcow2"
	case FormatIso:
		return "iso"
	}
}

func NewVolumeFormat(input string) VolumeFormat {
	switch input {
	default:
		return FormatUnknown
	case "raw":
		return FormatRaw
	case "qcow2":
		return FormatQcow2
	case "iso":
		return FormatIso
	}
}
