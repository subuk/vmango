package compute

type Size struct {
	Value uint64
	Unit  SizeUnit
}

func NewSize(value uint64, unit SizeUnit) Size {
	return Size{value, unit}
}

func (s Size) Bytes() uint64 {
	switch s.Unit {
	default:
		panic("unknown size unit")
	case SizeUnitB:
		return s.Value
	case SizeUnitK:
		return s.Value * 1024
	case SizeUnitM:
		return s.Value * 1024 * 1024
	case SizeUnitG:
		return s.Value * 1024 * 1024 * 1024
	}
}

func (s Size) M() uint64 {
	return s.Bytes() / 1024 / 1024
}

func (s Size) G() uint64 {
	return s.Bytes() / 1024 / 1024 / 1024
}
