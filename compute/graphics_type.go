package compute

type GraphicType int

const (
	GraphicTypeUnknown GraphicType = 0
	GraphicTypeNone                = 1
	GraphicTypeVnc                 = 2
	GraphicTypeSpice               = 3
)

func (GraphicType GraphicType) String() string {
	switch GraphicType {
	default:
		return "unknown"
	case GraphicTypeNone:
		return "none"
	case GraphicTypeVnc:
		return "vnc"
	case GraphicTypeSpice:
		return "spice"
	}
}

func NewGraphicType(input string) GraphicType {
	switch input {
	default:
		return GraphicTypeUnknown
	case "none":
		return GraphicTypeNone
	case "vnc":
		return GraphicTypeVnc
	case "spice":
		return GraphicTypeSpice
	}
}

func (GraphicType GraphicType) IsNone() bool {
	return GraphicType == GraphicTypeNone
}
