package libvirt

import (
	"fmt"
	"strconv"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

type NodeSettings struct {
	CdSuffix string
	Cache    bool
}

func ComputeSizeUnitToLibvirtUnit(input compute.SizeUnit) string {
	switch input {
	default:
		panic(fmt.Errorf("unknown size unit '%+v'", input))
	case compute.SizeUnitB:
		return "bytes"
	case compute.SizeUnitK:
		return "KiB"
	case compute.SizeUnitM:
		return "MiB"
	case compute.SizeUnitG:
		return "GiB"
	}
}

func ComputeSizeFromLibvirtSize(unit string, value uint64) compute.Size {
	switch unit {
	default:
		panic(fmt.Sprintf("unknown libvirt size unit '%s'", unit))
	case "bytes":
		return compute.NewSize(value, compute.SizeUnitB)
	case "KiB":
		return compute.NewSize(value, compute.SizeUnitK)
	case "MiB":
		return compute.NewSize(value, compute.SizeUnitM)
	case "GiB":
		return compute.NewSize(value, compute.SizeUnitG)
	}
}

func ParseCpuAffinity(input string) []uint {
	cpus := []uint{}
	for _, part := range strings.Split(input, ",") {
		if strings.Contains(part, "-") {
			rangeparams := strings.SplitN(part, "-", 2)
			if len(rangeparams) < 2 {
				// invalid cpu range
				return cpus
			}
			start, err := strconv.ParseUint(rangeparams[0], 10, 32)
			if err != nil {
				// cannot parse start cpu number
				return cpus
			}
			end, err := strconv.ParseUint(rangeparams[1], 10, 32)
			if err != nil {
				// cannot parse end cpu number
				return cpus
			}
			for cpu := start; cpu <= end; cpu++ {
				cpus = append(cpus, uint(cpu))
			}
		} else {
			cpu, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				// cannot parse
				return cpus
			}
			cpus = append(cpus, uint(cpu))
		}

	}
	return cpus
}

func getVolumeConfigByPath(conn *libvirt.Connect, path string) (*libvirtxml.StorageVolume, error) {
	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup volume")
	}
	virVolumeXml, err := virVolume.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get volume xml")
	}
	virVolumeConfig := &libvirtxml.StorageVolume{}
	if err := virVolumeConfig.Unmarshal(virVolumeXml); err != nil {
		return nil, util.NewError(err, "cannot unmarshal volume config")
	}
	return virVolumeConfig, nil
}

func getVolTargetFormatType(vol *libvirtxml.StorageVolume) string {
	if vol.Target != nil && vol.Target.Format != nil {
		return vol.Target.Format.Type
	}
	return "raw"
}
