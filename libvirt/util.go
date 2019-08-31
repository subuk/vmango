package libvirt

func ParseLibvirtSizeToMegabytes(unit string, value uint64) uint64 {
	switch unit {
	default:
		panic("unknown storage pool capacity unit")
	case "bytes":
		return value / 1024 / 1024
	}
}
