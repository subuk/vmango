package compute

type SizeUnit int

const (
	SizeUnitUnknown SizeUnit = iota
	SizeUnitB
	SizeUnitK
	SizeUnitM
	SizeUnitG
)

func (SizeUnit SizeUnit) String() string {
	switch SizeUnit {
	default:
		return "unknown"
	case SizeUnitB:
		return "B"
	case SizeUnitK:
		return "K"
	case SizeUnitM:
		return "M"
	case SizeUnitG:
		return "G"
	}
}

func NewSizeUnit(input string) SizeUnit {
	switch input {
	default:
		return SizeUnitUnknown
	case "B":
		return SizeUnitB
	case "K":
		return SizeUnitK
	case "M":
		return SizeUnitM
	case "G":
		return SizeUnitG
	}
}
