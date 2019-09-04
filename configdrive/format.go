package configdrive

type Format int

const (
	FormatUnknown   = Format(0)
	FormatNoCloud   = Format(1)
	FormatOpenstack = Format(2)
)

var AllFormats = []Format{
	FormatNoCloud,
	FormatOpenstack,
}

func AllFormatsStrings() []string {
	r := []string{}
	for _, format := range AllFormats {
		r = append(r, format.String())
	}
	return r
}

func (format Format) String() string {
	switch format {
	default:
		return "unknown"
	case FormatNoCloud:
		return "nocloud"
	case FormatOpenstack:
		return "openstack"
	}
}

func NewFormat(input string) Format {
	switch input {
	default:
		return FormatUnknown
	case "nocloud":
		return FormatNoCloud
	case "openstack":
		return FormatOpenstack
	}
}
