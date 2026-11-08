package libvirt

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"subuk/vmango/compute"

	libvirt "libvirt.org/go/libvirt"
	libvirtxml "libvirt.org/go/libvirtxml"
)

type NodeSettings struct {
	CdSuffix             string
	GraphicsListen       string
	Emulator             string
	QcowPreallocMetadata bool
	Cache                bool
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
	selected := map[uint]bool{}
	excluded := map[uint]bool{}
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		exclude := strings.HasPrefix(part, "^")
		part = strings.TrimPrefix(part, "^")
		bounds := strings.SplitN(part, "-", 2)
		start, err := strconv.ParseUint(bounds[0], 10, 32)
		if err != nil {
			continue
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.ParseUint(bounds[1], 10, 32)
			if err != nil || end < start {
				continue
			}
		}
		if end-start > 1<<20 {
			continue
		}
		for cpu := start; cpu <= end; cpu++ {
			if exclude {
				excluded[uint(cpu)] = true
			} else {
				selected[uint(cpu)] = true
			}
		}
	}
	cpus := []uint{}
	for cpu := range selected {
		if !excluded[cpu] {
			cpus = append(cpus, cpu)
		}
	}
	sort.Slice(cpus, func(i, j int) bool { return cpus[i] < cpus[j] })
	return cpus
}

func getVolumeConfigByPath(conn *libvirt.Connect, path string) (*libvirtxml.StorageVolume, error) {
	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return nil, repositoryError(err, "cannot lookup volume")
	}
	virVolumeXml, err := virVolume.GetXMLDesc(0)
	if err != nil {
		return nil, repositoryError(err, "cannot get volume xml")
	}
	virVolumeConfig := &libvirtxml.StorageVolume{}
	if err := virVolumeConfig.Unmarshal(virVolumeXml); err != nil {
		return nil, repositoryError(err, "cannot unmarshal volume config")
	}
	return virVolumeConfig, nil
}

func getVolTargetFormatType(vol *libvirtxml.StorageVolume) string {
	if vol.Target != nil && vol.Target.Format != nil {
		return vol.Target.Format.Type
	}
	return "raw"
}

func generateMacAddress(prefix string) string {
	prefix = strings.TrimSuffix(prefix, ":")
	parts := strings.Split(prefix, ":")

	for i := len(parts); i < 6; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(0xFF))
		if err != nil {
			panic(err)
		}
		parts = append(parts, fmt.Sprintf("%02x", n.Int64()))
	}
	return strings.Join(parts, ":")
}

func parseQemuDeviceArg(argv string) map[string]string {
	parts := strings.Split(argv, ",")
	params := map[string]string{}
	params[""] = parts[0]
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		params[kv[0]] = kv[1]
	}
	return params
}
