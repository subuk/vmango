package compute

import "testing"

func TestPhysicalCoreCapacitySMTAndIdentity(t *testing.T) {
	for _, threads := range []int{1, 2} {
		i := testInventory()
		i.Node.Cpus = nil
		i.Host.CPUReserve = 2
		i.Host.CPUOvercommit = 4
		for socket := 0; socket < 2; socket++ {
			for die := 0; die < 2; die++ {
				for core := 0; core < 4; core++ {
					for thread := 0; thread < threads; thread++ {
						i.Node.Cpus = append(i.Node.Cpus, NodeCpu{SocketId: socket, DieId: die, CoreId: core, Online: true, TopologyKnown: true})
					}
				}
			}
		}
		typ := testType()
		typ.VCPUs = 56
		if fit, err := Fit(i, typ, ""); err != nil || fit.FreeCPU != 0 {
			t.Fatalf("SMT %d: %+v %v", threads, fit, err)
		}
		typ.VCPUs++
		if _, err := Fit(i, typ, ""); err == nil {
			t.Fatal("SMT inflated physical capacity")
		}
		i.Node.Cpus[0].TopologyKnown = false
		if _, err := Fit(i, typ, ""); err == nil {
			t.Fatal("missing topology accepted")
		}
	}
	n := &Node{Cpus: []NodeCpu{{CoreId: 0, Online: true, TopologyKnown: true}, {CoreId: 1, Online: false, TopologyKnown: true}}}
	if c, e := n.PhysicalCoreCount(); e != nil || c != 1 {
		t.Fatal(c, e)
	}
}
func TestOvercommittedHostAllowsOnlyCompatibleReductions(t *testing.T) {
	i := testInventory()
	i.Machines = []*VirtualMachine{{Id: "old", VCpus: 10, Memory: NewSize(12, SizeUnitG), Metadata: &MachineMetadata{CPURequest: 1, MemoryRequestBytes: 12 << 30}}, {Id: "external", VCpus: 8, Memory: NewSize(2, SizeUnitG), State: StateStopped}}
	typ := testType()
	typ.VCPUs = 9
	typ.MemoryBytes = 10 << 30
	if _, err := Fit(i, typ, "old"); err != nil {
		t.Fatal("capacity-reducing change rejected", err)
	}
	typ.VCPUs = 11
	if _, err := Fit(i, typ, "old"); err == nil {
		t.Fatal("overcommitted growth accepted")
	}
	typ.VCPUs = 1
	if _, err := Fit(i, typ, ""); err == nil {
		t.Fatal("discounted legacy CPU metadata used")
	}
	i.Host.CPUOvercommit = int(^uint(0) >> 1)
	if _, err := Fit(i, typ, ""); err == nil {
		t.Fatal("overflow accepted")
	}
	i.Host.CPUOvercommit = 1
	i.Host.CPUReserve = 9
	if _, err := Fit(i, typ, ""); err == nil {
		t.Fatal("reserve exceeds cores")
	}
}
