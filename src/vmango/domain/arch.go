package domain

const (
	ARCH_UNKNOWN = 0
	ARCH_X86_64  = 1
	ARCH_X86     = 4
)

type HWArch int

func (arch HWArch) MarshalJSON() ([]byte, error) {
	data := []byte(`"` + arch.String() + `"`)
	return data, nil
}

func (arch HWArch) String() string {
	switch arch {
	default:
		return "unknown"
	case ARCH_X86_64:
		return "x86_64"
	case ARCH_X86:
		return "x86"
	}
}

func ParseHWArch(in string) HWArch {
	switch in {
	default:
		return ARCH_UNKNOWN
	case "amd64", "x86_64":
		return ARCH_X86_64
	case "i386", "x86":
		return ARCH_X86
	}

}
