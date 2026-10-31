package compute

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestVolumeIDCanonicalRoundTrip(t *testing.T) {
	for _, address := range []VolumeAddress{{Host: "host.a", Path: "/pool/disk.qcow2"}, {Host: "хост/一", Path: "/pool/a.b/диск 一"}} {
		got, err := ParseVolumeID(address.ID())
		if err != nil || got != address {
			t.Fatalf("address did not round trip: %+v %v", got, err)
		}
		other := address
		other.Host += "-other"
		if address.ID() == other.ID() {
			t.Fatal("equal paths on different hosts share a public ID")
		}
	}
	for _, id := range []string{"", "L3BhdGg", "YQ.", ".YQ", "YQ.YQ.extra", "YQ==.YQ", "YR.YQ", "YQ.YR", "YQ." + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 4097)))} {
		if _, err := ParseVolumeID(id); !errors.Is(err, ErrInvalid) {
			t.Errorf("malformed ID accepted: %q: %v", id, err)
		}
	}
}

type addressVolumeRepo struct {
	VolumeRepository
	reads  []VolumeAddress
	getErr error
	failed map[string]bool
}

func (r *addressVolumeRepo) Get(path, node string) (*Volume, error) {
	r.reads = append(r.reads, VolumeAddress{Host: node, Path: path})
	return &Volume{NodeId: node, Path: path, Size: NewSize(1, SizeUnitG)}, r.getErr
}
func (r *addressVolumeRepo) List(options VolumeListOptions) ([]*Volume, error) {
	if r.failed[options.NodeIds[0]] {
		return nil, ErrUnavailable
	}
	return []*Volume{{NodeId: options.NodeIds[0], Path: "/same", Size: NewSize(1, SizeUnitG)}}, nil
}

type addressVMRepo struct {
	VirtualMachineRepository
	gets int
}

func (r *addressVMRepo) List(options VirtualMachineListOptions) ([]*VirtualMachine, error) {
	if options.NodeIds[0] != "a" {
		return []*VirtualMachine{}, nil
	}
	return []*VirtualMachine{{Id: "vm", NodeId: "a"}}, nil
}
func (r *addressVMRepo) Get(id, node string) (*VirtualMachine, error) {
	r.gets++
	return &VirtualMachine{Id: id, NodeId: node}, nil
}

func TestVolumeResolutionChecksMembershipAndMachineLocalityBeforeStorage(t *testing.T) {
	repo := &addressVolumeRepo{}
	clouds := &CloudService{Definitions: []Cloud{{ID: "one", Hosts: []CloudHost{{Node: "a"}, {Node: "b"}}}, {ID: "two", Hosts: []CloudHost{{Node: "c"}}}}, Volumes: NewVolumeService(repo), VMs: NewVirtualMachineService(&addressVMRepo{})}
	for _, request := range []struct{ cloud, host string }{{"missing", "a"}, {"one", "c"}, {"one", "unknown"}} {
		if _, err := clouds.GetVolume(request.cloud, (VolumeAddress{Host: request.host, Path: "/same"}).ID()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("wrong membership: %v", err)
		}
	}
	if len(repo.reads) != 0 {
		t.Fatal("membership rejection accessed storage", repo.reads)
	}
	if _, err := clouds.MachineVolume(context.Background(), "one", "vm", (VolumeAddress{Host: "b", Path: "/same"}).ID()); !errors.Is(err, ErrInvalid) {
		t.Fatal("cross-host disk accepted", err)
	}
	if len(repo.reads) != 0 {
		t.Fatal("cross-host VM request accessed another host's storage", repo.reads)
	}
	if _, err := clouds.MachineVolume(context.Background(), "one", "vm", (VolumeAddress{Host: "a", Path: "/same"}).ID()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repo.reads, []VolumeAddress{{Host: "a", Path: "/same"}}) {
		t.Fatal(repo.reads)
	}
	repo.getErr = ErrUnavailable
	if _, err := clouds.GetVolume("one", (VolumeAddress{Host: "a", Path: "/same"}).ID()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("availability failure changed", err)
	}
}

func TestVolumeInventoryPreservesSuccessfulHostsAndReportsEveryFailure(t *testing.T) {
	repo := &addressVolumeRepo{failed: map[string]bool{"b": true}}
	clouds := &CloudService{Definitions: []Cloud{{ID: "one", Hosts: []CloudHost{{Node: "a"}, {Node: "b"}}}, {ID: "two", Hosts: []CloudHost{{Node: "c"}}}}, Volumes: NewVolumeService(repo)}
	got, err := clouds.ListVolumes("one", false)
	if err != nil || len(got.Volumes) != 1 || got.Volumes[0].NodeId != "a" || !reflect.DeepEqual(got.UnavailableHosts, []string{"b"}) {
		t.Fatalf("partial inventory lost: %+v %v", got, err)
	}
	repo.failed["a"] = true
	got, err = clouds.ListVolumes("one", false)
	if err != nil || len(got.Volumes) != 0 || !reflect.DeepEqual(got.UnavailableHosts, []string{"a", "b"}) {
		t.Fatalf("all-unavailable appeared empty: %+v %v", got, err)
	}
	got, err = clouds.ListVolumes("one", false, "b")
	if err != nil || !reflect.DeepEqual(got.UnavailableHosts, []string{"b"}) {
		t.Fatal("host selector enumerated unrelated hosts", got, err)
	}
	if _, err = clouds.ListVolumes("one", false, "c"); !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong-cloud host filter accepted", err)
	}
}

func TestSubmittedDiskHostCannotBypassCommandMachineLocator(t *testing.T) {
	volumes := &addressVolumeRepo{}
	machines := &addressVMRepo{}
	clouds := &CloudService{Definitions: []Cloud{{ID: "one", Hosts: []CloudHost{{Node: "a"}, {Node: "b"}}}}, Volumes: NewVolumeService(volumes), VMs: NewVirtualMachineService(machines)}
	handler := &CommandHandler{Clouds: clouds}
	for _, kind := range []string{"machine.attach-disk", "machine.detach-disk"} {
		input := InfrastructureCommand{CloudID: "one", Node: "b", VM: "vm", Path: "/same", Disk: &VirtualMachineAttachedVolume{Path: "/same"}}
		_, _, err := handler.prepareCloud(NewCommand(kind, input), input)
		if !errors.Is(err, ErrInvalid) {
			t.Fatal("submitted Node bypassed independent VM resolution", kind, err)
		}
	}
	if machines.gets != 2 || len(volumes.reads) != 0 {
		t.Fatal("locator did not verify placement before disk access", machines.gets, volumes.reads)
	}
	input := InfrastructureCommand{CloudID: "one", Node: "outside", VM: "vm", Path: "/same"}
	if _, _, err := handler.prepareCloud(NewCommand("machine.detach-disk", input), input); !errors.Is(err, ErrUnknownNode) || machines.gets != 2 {
		t.Fatal("unknown submitted host reached the machine locator", err, machines.gets)
	}
}
