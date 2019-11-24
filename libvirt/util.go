package libvirt

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseLibvirtSizeToBytes(unit string, value uint64) uint64 {
	switch unit {
	default:
		panic(fmt.Sprintf("unknown libvirt size unit '%s'", unit))
	case "bytes":
		return value
	case "KiB":
		return value * 1024
	case "MiB":
		return value * 1024 * 1024
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
