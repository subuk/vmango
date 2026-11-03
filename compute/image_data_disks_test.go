package compute

import (
	"encoding/json"
	"testing"
)

func TestImageDataDisksPlacementAndCapacity(t *testing.T) {
	image := Image{CloudID: "c", ID: "image", Arch: ArchAmd64, Defaults: ImageDefaults{Initialization: "none"}, SourcePath: "/image", DataDisks: []ImageDataDisk{{Name: "userdata", SizeBytes: 16 << 30}, {Name: "cache", SizeBytes: 2 << 30}}}
	images := NewImageService(pinnedImages{image})
	volumes := &imageDataDiskVolumes{provisioningVolumes: provisioningVolumes{failAt: -1}}
	manager := NewVirtualMachineManager(NewVirtualMachineService(&cloudImageVMBackend{}), NewVolumeService(volumes), nil, nil, images)
	service := CloudService{Manager: manager, Volumes: manager.volumes}
	host := CloudHost{Node: "a", Zone: "z", Networks: map[string]NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]StorageBinding{"fast": {Pool: "pool", Format: VolumeFormatQcow2}}}
	cloud := Cloud{ID: "c", Images: []string{"image"}, Zones: []CatalogEntry{{ID: "z"}}, Networks: []CatalogEntry{{ID: "lan"}}, StorageClasses: []CatalogEntry{{ID: "fast"}}, InstanceTypes: []InstanceType{testType()}, Hosts: []CloudHost{host}}
	inventory := testInventory()
	inventory.Host = host
	inventory.Node.CpuArch = ArchAmd64
	inventory.Pools = []*VolumePool{{Name: "pool", Free: NewSize(28, SizeUnitG)}}
	spec := MachineCreateSpec{Name: "new", Image: "image", InstanceType: "small", Zone: "z", Network: "lan", StorageClass: "fast"}
	if err := service.ValidateCreate(cloud, spec); err != nil {
		t.Fatal(err)
	}
	command, err := service.ResolveCreate(cloud, spec, images, []HostInventory{inventory})
	if err != nil {
		t.Fatal(err)
	}
	if len(command.NewVolumes) != 2 || len(volumes.created) != 0 {
		t.Fatalf("disks: %+v", command.NewVolumes)
	}
	for n, d := range command.NewVolumes {
		want := image.DataDisks[n]
		if d.Name != "new_"+want.Name || d.Alias != want.Name || d.Size.Bytes() != want.SizeBytes || d.Pool != "pool" || d.StorageClass != "fast" || d.Format != VolumeFormatQcow2 || d.DeviceBus != DeviceBusVirtio || !d.ImageDisk {
			t.Fatalf("disk: %+v", d)
		}
	}
	inventory.Pools[0].Free = NewSize((28<<30)-1, SizeUnitB)
	if _, err := service.ResolveCreate(cloud, spec, images, []HostInventory{inventory}); err == nil {
		t.Fatal("mandatory storage ignored")
	}
	for _, name := range []string{"userdata", "root"} {
		spec.Disks = []DataDiskSpec{{Name: name, StorageClass: "fast", SizeBytes: 1 << 30}}
		if err := service.ValidateCreate(cloud, spec); err == nil {
			t.Fatalf("collision %s accepted", name)
		}
	}
}

type imageDataDiskVolumes struct{ provisioningVolumes }

func (v *imageDataDiskVolumes) Get(path, node string) (*Volume, error) {
	volume, err := v.provisioningVolumes.Get(path, node)
	volume.Format = VolumeFormatQcow2
	return volume, err
}

func TestImageDataDisksSnapshotIsolation(t *testing.T) {
	image := Image{CloudID: "c", ID: "android", DataDisks: []ImageDataDisk{{Name: "userdata", SizeBytes: 16 << 30}}}
	service := NewImageService(nil).ForCloud("c").Snapshot([]Image{image}, nil, false)
	image.DataDisks[0].SizeBytes = 1
	first, err := service.Get("android")
	if err != nil {
		t.Fatal(err)
	}
	if first.DataDisks[0].SizeBytes != 16<<30 {
		t.Fatal("snapshot aliases input")
	}
	first.DataDisks[0].Name = "changed"
	listed, _ := service.List()
	listed[0].DataDisks[0].SizeBytes = 2
	second, _ := service.Get("android")
	if second.DataDisks[0].Name != "userdata" || second.DataDisks[0].SizeBytes != 16<<30 {
		t.Fatal("snapshot aliases outputs")
	}
	var legacy Image
	if err := json.Unmarshal([]byte(`{"ID":"android","CloudID":"c"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	pinned := NewImageService(pinnedImages{image}).ForCloud("c").Snapshot([]Image{legacy}, nil, false)
	old, _ := pinned.Get("android")
	if len(old.DataDisks) != 0 {
		t.Fatal("old accepted image acquired current disks")
	}
}

type imageDiskOfferNode struct{ NodeRepository }

func (imageDiskOfferNode) Get(string, NodeGetOptions) (*Node, error) {
	n := testInventory().Node
	n.CpuArch = ArchAmd64
	return n, nil
}

type imageDiskOfferPools struct {
	VolumePoolRepository
	free uint64
}

func (p imageDiskOfferPools) List(VolumePoolListOptions) ([]*VolumePool, error) {
	return []*VolumePool{{Name: "pool", Free: NewSize(p.free, SizeUnitB)}}, nil
}
func TestImageDataDisksOffersRequireSpaceAndVirtio(t *testing.T) {
	image := Image{CloudID: "c", ID: "image", Arch: ArchAmd64, SourcePath: "/source", DataDisks: []ImageDataDisk{{Name: "userdata", SizeBytes: 16 << 30}}}
	images := NewImageService(pinnedImages{image})
	volumes := NewVolumeService(&imageDataDiskVolumes{})
	vms := NewVirtualMachineService(&cloudImageVMBackend{})
	s := CloudService{VMs: vms, Volumes: volumes, Nodes: NewNodeService(imageDiskOfferNode{}), Manager: NewVirtualMachineManager(vms, volumes, nil, nil, images)}
	cloud := Cloud{ID: "c", Images: []string{"image"}, InstanceTypes: []InstanceType{testType()}, Networks: []CatalogEntry{{ID: "lan"}}, StorageClasses: []CatalogEntry{{ID: "fast"}}, Hosts: []CloudHost{{Node: "a", Zone: "z", Networks: map[string]NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]StorageBinding{"fast": {Pool: "pool", Format: VolumeFormatRaw}}}}}
	for _, free := range []uint64{26 << 30, (26 << 30) - 1} {
		s.Pools = NewVolumePoolService(imageDiskOfferPools{free: free})
		offers, err := s.Offers(cloud)
		if err != nil {
			t.Fatal(err)
		}
		if (len(offers) > 0) != (free == 26<<30) {
			t.Fatalf("free %d: offers %+v", free, offers)
		}
	}
	caps, _ := (&cloudImageVMBackend{}).Capabilities("a", ArchAmd64)
	caps.DiskBuses = []DeviceBus{DeviceBusScsi}
	if image.Compatible(caps) {
		t.Fatal("required VirtIO disks offered on unsupported host")
	}
}
