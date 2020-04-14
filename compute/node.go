package compute

type NodeNumaCore struct {
	SocketId int
	CpuIds   []int
}

type NodeNuma struct {
	Memory  Size
	Pages4k uint64
	Pages2m uint64
	Pages1g uint64
	Cores   map[int]NodeNumaCore
}

type Node struct {
	Id        string
	Hostname  string
	CpuArch   Arch
	CpuVendor string
	CpuModel  string
	CpuInfo   string
	Iommu     bool
	Numas     map[int]NodeNuma
}

func (n *Node) Memory() Size {
	bytes := uint64(0)
	for _, numa := range n.Numas {
		bytes += numa.Memory.Bytes()
	}
	return Size{Value: bytes, Unit: SizeUnitB}
}
