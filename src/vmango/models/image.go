package models

import (
	"time"
)

const (
	IMAGE_FMT_RAW = iota
)
const (
	IMAGE_ARCH_X86_64 = iota
	IMAGE_ARCH_X86    = iota
)

type Image struct {
	Name     string
	Arch     int
	Size     int64
	Type     int
	Date     time.Time
	Filename string
}

func (image *Image) String() string {
	return image.Name
}

func (image *Image) SizeMegabytes() int {
	return int(image.Size / 1024 / 1024)
}

func (image *Image) ArchString() string {
	switch image.Arch {
	default:
		return "unknown"
	case IMAGE_ARCH_X86_64:
		return "amd64"
	case IMAGE_ARCH_X86:
		return "i386"
	}
}

func (image *Image) ArchString2() string {
	switch image.Arch {
	default:
		return "unknown"
	case IMAGE_ARCH_X86_64:
		return "x86_64"
	case IMAGE_ARCH_X86:
		return "x86"
	}
}

func (image *Image) TypeString() string {
	switch image.Type {
	default:
		return "unknown"
	case IMAGE_FMT_RAW:
		return "raw"
	}
}
