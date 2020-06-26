package compute

type Arch int

const (
	ArchUnknown = Arch(0)
	ArchAmd64   = Arch(1)
	ArchI386    = Arch(2)
)

var ValidArches = []Arch{ArchAmd64, ArchI386}

func (arch Arch) String() string {
	switch arch {
	default:
		return "unknown"
	case ArchAmd64:
		return "x86_64"
	case ArchI386:
		return "i386"
	}
}

func NewArch(input string) Arch {
	switch input {
	default:
		return ArchUnknown
	case "x86_64":
		return ArchAmd64
	case "i386":
		return ArchI386
	}
}
