package compute

type VideoModel int

const (
	VideoModelUnknown VideoModel = 0
	VideoModelNone               = 1
	VideoModelCirrus             = 2
	VideoModelQxl                = 3
	VideoModelVirtio             = 4
)

func (VideoModel VideoModel) String() string {
	switch VideoModel {
	default:
		return "unknown"
	case VideoModelNone:
		return "none"
	case VideoModelCirrus:
		return "cirrus"
	case VideoModelQxl:
		return "qxl"
	case VideoModelVirtio:
		return "virtio"
	}
}

func NewVideoModel(input string) VideoModel {
	switch input {
	default:
		return VideoModelUnknown
	case "none":
		return VideoModelNone
	case "cirrus":
		return VideoModelCirrus
	case "qxl":
		return VideoModelQxl
	case "virtio":
		return VideoModelVirtio
	}
}

func (VideoModel VideoModel) IsNone() bool {
	return VideoModel == VideoModelNone
}
