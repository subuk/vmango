package compute

import (
	"errors"
	"fmt"
	"testing"
)

type provisioningVMs struct {
	VirtualMachineRepository
	exists   bool
	saved    bool
	deleted  bool
	failSave bool
}

func (v *provisioningVMs) ValidateNew(*VirtualMachine) error {
	if v.exists {
		return ErrConflict
	}
	return nil
}
func (v *provisioningVMs) Save(*VirtualMachine) error {
	if v.failSave {
		return fmt.Errorf("define failed")
	}
	v.saved = true
	return nil
}
func (v *provisioningVMs) Delete(string, string) error { v.deleted = true; return nil }

type provisioningVolumes struct {
	VolumeRepository
	created   []string
	failAt    int
	protected bool
}

func (v *provisioningVolumes) ValidateCreate(VolumeCreateParams) error { return nil }
func (v *provisioningVolumes) Create(p VolumeCreateParams) (*Volume, error) {
	if len(v.created) == v.failAt {
		return nil, fmt.Errorf("storage failure")
	}
	v.created = append(v.created, p.Name)
	return &Volume{NodeId: p.NodeId, Path: "/pool/" + p.Name}, nil
}
func (v *provisioningVolumes) Get(path, node string) (*Volume, error) {
	return &Volume{Path: path, NodeId: node, Size: NewSize(10, SizeUnitG), Metadata: VolumeMetadata{Protected: v.protected}}, nil
}
func (v *provisioningVolumes) Resize(string, string, Size) error {
	return fmt.Errorf("unexpected backend mutation")
}
func (v *provisioningVolumes) Delete(string, string) error {
	return fmt.Errorf("unexpected backend mutation")
}

type failingEvent struct{}

func (failingEvent) Publish(Event) error { return fmt.Errorf("hook failed") }

func TestProvisioningFailureReportsEffectsWithoutRollback(t *testing.T) {
	for _, stage := range []string{"create-volume", "define-vm", "created-event"} {
		t.Run(stage, func(t *testing.T) {
			vms := &provisioningVMs{failSave: stage == "define-vm"}
			vols := &provisioningVolumes{failAt: -1}
			if stage == "create-volume" {
				vols.failAt = 1
			}
			manager := NewVirtualMachineManager(NewVirtualMachineService(vms), NewVolumeService(vols), failingEvent{}, nil, nil)
			vm := &VirtualMachine{Id: "test", Name: "test", NodeId: "node", VCpus: 2, Memory: NewSize(1, SizeUnitG), Graphic: VirtualMachineGraphic{Type: GraphicTypeNone}, VideoModel: VideoModelNone}
			disks := []VirtualMachineManagerCreatedVolumeParams{{Name: "one", Pool: "pool", Format: VolumeFormatRaw, Size: NewSize(1, SizeUnitG), DeviceType: DeviceTypeDisk, DeviceBus: DeviceBusVirtio}, {Name: "two", Pool: "pool", Format: VolumeFormatRaw, Size: NewSize(1, SizeUnitG), DeviceType: DeviceTypeDisk, DeviceBus: DeviceBusVirtio}}
			err := manager.Create(vm, nil, disks, false)
			var partial *ProvisioningError
			if !errors.As(err, &partial) {
				t.Fatalf("expected provisioning error, got %v", err)
			}
			if partial.Stage != stage || partial.Node != "node" || partial.VM != "test" || len(partial.Volumes) != len(vols.created) || partial.VMCreated != vms.saved || vms.deleted {
				t.Fatalf("incorrect partial result: %+v", partial)
			}
		})
	}
}
func TestDuplicateCreateRejectedBeforeDiskMutation(t *testing.T) {
	vms := &provisioningVMs{exists: true}
	vols := &provisioningVolumes{failAt: -1}
	manager := NewVirtualMachineManager(NewVirtualMachineService(vms), NewVolumeService(vols), failingEvent{}, nil, nil)
	if err := manager.Create(&VirtualMachine{Id: "existing", Name: "existing", NodeId: "node"}, nil, nil, false); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if len(vols.created) != 0 || vms.saved {
		t.Fatal("duplicate creation mutated resources")
	}
}
func TestProtectedVolumesAndShrinkRejected(t *testing.T) {
	repo := &provisioningVolumes{protected: true}
	s := NewVolumeService(repo)
	if err := s.Delete("/disk", "node"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := s.Resize("/disk", "node", NewSize(20, SizeUnitG)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	repo.protected = false
	if err := s.Resize("/disk", "node", NewSize(1, SizeUnitG)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
