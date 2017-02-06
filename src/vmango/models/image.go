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

type ImageList []*Image

func (images *ImageList) DistinctHypervisors() []string {
	hypervisors := map[string]struct{}{}
	for _, image := range *images {
		hypervisors[image.Hypervisor] = struct{}{}
	}
	result := []string{}
	for name := range hypervisors {
		result = append(result, name)
	}
	return result
}

func (images *ImageList) Distinct() *ImageList {
	result := ImageList{}
	imageNames := map[string]struct{}{}
	for _, image := range *images {
		if _, exist := imageNames[image.FullName]; exist {
			continue
		}
		imageNames[image.FullName] = struct{}{}
		result = append(result, image)
	}
	return &result
}

type Image struct {
	OS         string
	Arch       int
	Size       uint64
	Type       int
	Date       time.Time
	FullName   string
	FullPath   string
	PoolName   string
	Hypervisor string
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
