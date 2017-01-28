package models

import (
	"os"
	"strings"
	"time"
)

const (
	IMAGE_FMT_RAW   = iota
	IMAGE_FMT_QCOW2 = iota
)
const (
	IMAGE_ARCH_X86_64 = iota
	IMAGE_ARCH_X86    = iota
)

type Image struct {
	OS       string
	Arch     int
	Size     uint64
	Type     int
	Date     time.Time
	FullName string
	FullPath string
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

func (image *Image) Stream() (*os.File, error) {
	return os.Open(image.FullPath)
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
	case IMAGE_FMT_QCOW2:
		return "qcow2"
	}
}
