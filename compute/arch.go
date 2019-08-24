package compute

type Arch int

const (
	ArchUnknown = Arch(0)
	ArchAmd64   = Arch(1)
)

func (arch Arch) String() string {
	switch arch {
	default:
		return "unknown"
	case ArchAmd64:
		return "x86_64"
	}
}
