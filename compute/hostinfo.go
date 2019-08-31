package compute

type HostInfoRepository interface {
	Get() (*HostInfo, error)
}

type HostInfoNumaCore struct {
	SocketId int
	CpuIds   []int
}

type HostInfoNuma struct {
	Memory  uint64 // KiB
	Pages4k uint64
	Pages2m uint64
	Pages1g uint64
	Cores   map[int]HostInfoNumaCore
}

type HostInfo struct {
	Hostname  string
	CpuArch   Arch
	CpuVendor string
	CpuModel  string
	Iommu     bool
	Numas     map[int]HostInfoNuma
}
