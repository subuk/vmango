package compute

type NodeCpuPin struct {
	VmId string
	Desc string
}

type NodeNuma struct {
	Memory      Size
	Pages4k     uint64
	Pages4kFree uint64
	Pages2m     uint64
	Pages2mFree uint64
	Pages1g     uint64
	Pages1gFree uint64
}

func (n *NodeNuma) Pages4kSize() Size {
	return NewSize(n.Pages4k*4, SizeUnitK)
}

func (n *NodeNuma) Pages4kFreeSize() Size {
	return NewSize(n.Pages4kFree*4, SizeUnitK)
}

func (n *NodeNuma) Pages4kUsedSize() Size {
	return NewSize((n.Pages4k-n.Pages4kFree)*4, SizeUnitK)
}

func (n *NodeNuma) Pages4kUsedPercent() int {
	if n.Pages4k == 0 {
		return 0
	}
	return int(100 * n.Pages4kUsedSize().Bytes() / n.Pages4kSize().Bytes())
}

func (n *NodeNuma) Pages2mSize() Size {
	return NewSize(n.Pages2m*2, SizeUnitM)
}

func (n *NodeNuma) Pages2mFreeSize() Size {
	return NewSize(n.Pages2mFree*2, SizeUnitM)
}

func (n *NodeNuma) Pages2mUsedSize() Size {
	return NewSize((n.Pages2m-n.Pages2mFree)*2, SizeUnitM)
}

func (n *NodeNuma) Pages2mUsedPercent() int {
	if n.Pages2m == 0 {
		return 0
	}
	return int(100 * n.Pages2mUsedSize().Bytes() / n.Pages2mSize().Bytes())
}

func (n *NodeNuma) Pages1gSize() Size {
	return NewSize(n.Pages1g, SizeUnitG)
}

func (n *NodeNuma) Pages1gFreeSize() Size {
	return NewSize(n.Pages1gFree, SizeUnitG)
}

func (n *NodeNuma) Pages1gUsedSize() Size {
	return NewSize(n.Pages1g-n.Pages1gFree, SizeUnitG)
}

func (n *NodeNuma) Pages1gUsedPercent() int {
	if n.Pages1g == 0 {
		return 0
	}
	return int(100 * n.Pages1gUsedSize().Bytes() / n.Pages1gSize().Bytes())
}

type NodeCpu struct {
	SocketId int
	CoreId   int
	NumaId   int
	Pins     []NodeCpuPin
}

type Node struct {
	Id             string
	Hostname       string
	CpuArch        Arch
	CpuVendor      string
	CpuModel       string
	CpuInfo        string
	ThreadsPerCore int
	Iommu          bool
	Numas          []NodeNuma
	Cpus           []NodeCpu
}

func (n *Node) Memory() Size {
	bytes := uint64(0)
	for _, numa := range n.Numas {
		bytes += numa.Memory.Bytes()
	}
	return Size{Value: bytes, Unit: SizeUnitB}
}

func (n *Node) Has2mPages() bool {
	for _, numa := range n.Numas {
		if numa.Pages2m > 0 {
			return true
		}
	}
	return false
}

func (n *Node) Has1gPages() bool {
	for _, numa := range n.Numas {
		if numa.Pages1g > 0 {
			return true
		}
	}
	return false
}
