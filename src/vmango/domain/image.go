package domain

import (
	"strings"
	"time"
)

const (
	IMAGE_FMT_UNKNOWN = iota
	IMAGE_FMT_RAW     = iota
	IMAGE_FMT_QCOW2   = iota
)

type ImageList []*Image

type Image struct {
	Id       string
	OS       string
	Arch     HWArch
	Size     uint64
	Type     int
	Date     time.Time
	PoolName string
}

func (image *Image) String() string {
	return image.OS
}

func (image *Image) OSName() string {
	return strings.Split(image.OS, "-")[0]
}
func (image *Image) OSVersion() string {
	return strings.Split(image.OS, "-")[1]
}

func (image *Image) SizeMegabytes() int {
	return int(image.Size / 1024 / 1024)
}

func (image *Image) TypeString() string {
	switch image.Type {
	default:
		return "unknown"
	case IMAGE_FMT_RAW:
		return "raw"
	case IMAGE_FMT_QCOW2:
		return "qcow2"
	}
}

func ParseImageFormat(in string) int {
	switch strings.TrimSuffix(in, ".img") {
	default:
		return IMAGE_FMT_UNKNOWN
	case "raw":
		return IMAGE_FMT_RAW
	case "qcow2":
		return IMAGE_FMT_QCOW2
	}
}
