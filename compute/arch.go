package compute

type Arch int

const (
	ArchUnknown = Arch(0)
	ArchAmd64   = Arch(1)
	ArchAarch64 = Arch(2)
)

func (arch Arch) String() string {
	switch arch {
	default:
		return "unknown"
	case ArchAmd64:
		return "x86_64"
	case ArchAarch64:
		return "aarch64"
	}
}

func NewArch(input string) Arch {
	switch input {
	default:
		return ArchUnknown
	case "x86_64":
		return ArchAmd64
	case "aarch64":
		return ArchAarch64
	}
}
