package api

import (
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"net/http"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/libvirt/hardware"
	"subuk/vmango/tasks"
	"testing"
)

func TestHardwareProfileAdministrationIsTypedAndCloudScoped(t *testing.T) {
	cfg := apiTestConfig(t)
	cfg.Clouds = []config.CloudConfig{{ID: "local", HardwareProfiles: []config.HardwareProfileConfig{{ID: "linux", DiskBus: "scsi"}}, Images: []config.ImageConfig{{ID: "image", HardwareProfile: "linux"}}}, {ID: "other", HardwareProfiles: []config.HardwareProfileConfig{{ID: "private-profile"}}}}
	snapshot := compute.AdministrationSnapshot{Clouds: []compute.AdministrationCloud{{ID: "local", BackendKind: "libvirt"}, {ID: "other", BackendKind: "libvirt"}, {ID: "external", BackendKind: "external"}}, Images: []compute.AdministrationImage{{ID: "image", CloudID: "local"}}}
	h, err := newScopedTestAPI(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil, testAuthService(t, cfg), newTestTaskService(t, &compute.CommandHandler{}), Services{Administration: compute.NewAdministrationService(snapshot, nil)})
	if err != nil {
		t.Fatal(err)
	}
	w := apiRequest(h, "GET", "/api/v1/administration/clouds/local/hardware-profiles", "", true)
	var profiles []hardware.Profile
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &profiles) != nil || len(profiles) != 1 || profiles[0].ID != "linux" || profiles[0].DiskController != "virtio-scsi" || profiles[0].ConfigDrive.Format != "nocloud" {
		t.Fatal(w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "private-profile") {
		t.Fatal("cross-cloud profile exposure")
	}
	w = apiRequest(h, "GET", "/api/v1/administration/clouds/local/images/image/hardware-profile", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"linux"`) {
		t.Fatal(w.Code, w.Body)
	}
	for _, path := range []string{"/api/v1/administration/clouds/external/hardware-profiles", "/api/v1/administration/clouds/missing/hardware-profiles", "/api/v1/administration/clouds/other/images/image/hardware-profile"} {
		w = apiRequest(h, "GET", path, "", true)
		if w.Code != 404 {
			t.Fatal(path, w.Code, w.Body)
		}
	}
}

type policyRejectionBackend struct {
	compute.VirtualMachineRepository
	compute.BackendMachineRepository
	adoptionErr error
	diskErr     error
	managed     bool
}

func (b *policyRejectionBackend) Get(id, node string) (*compute.VirtualMachine, error) {
	vm := &compute.VirtualMachine{Id: id, NodeId: node, Name: "Existing", VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateStopped}
	if b.managed {
		vm.Metadata = &compute.MachineMetadata{Cloud: "local", Zone: "zone"}
	}
	return vm, nil
}
func (b *policyRejectionBackend) List(options compute.VirtualMachineListOptions) ([]*compute.VirtualMachine, error) {
	vm, err := b.Get("existing", options.NodeIds[0])
	return []*compute.VirtualMachine{vm}, err
}
func (b *policyRejectionBackend) ValidateAppliedPolicy(*compute.VirtualMachine) error {
	if b.managed {
		return nil
	}
	return fmt.Errorf("machine requires explicit adoption with a compatible hardware profile")
}
func (b *policyRejectionBackend) PrepareAdoption(*compute.VirtualMachine, string, string) (json.RawMessage, error) {
	return nil, b.adoptionErr
}
func (b *policyRejectionBackend) ResolveDiskAttachment(*compute.VirtualMachine, *compute.VirtualMachineAttachedVolume) error {
	return b.diskErr
}

func policyRejectionAPI(t *testing.T, backend *policyRejectionBackend) (http.Handler, *tasks.Service) {
	t.Helper()
	cfg := apiTestConfig(t)
	vms := compute.NewVirtualMachineService(backend)
	vms.CloudMode = true
	clouds := &compute.CloudService{Definitions: []compute.Cloud{{ID: "local", Hosts: []compute.CloudHost{{Node: "node", Zone: "zone"}}}}, VMs: vms}
	manager := &compute.VirtualMachineManager{Clouds: clouds}
	taskService := newTestTaskService(t, &compute.CommandHandler{Clouds: clouds, VMs: vms, Manager: manager})
	h, err := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, vms, manager, nil, testAuthService(t, cfg), taskService, Services{Namespaces: testNamespaceRegistry(), Workloads: &compute.WorkloadService{}})
	if err != nil {
		t.Fatal(err)
	}
	return h, taskService
}

func TestAdoptionPolicyRejectionsAreActionableBeforeTaskAcceptance(t *testing.T) {
	message := "Selected profile is incompatible with the existing disk bus; choose a compatible profile."
	h, service := policyRejectionAPI(t, &policyRejectionBackend{adoptionErr: &compute.ValidationError{Field: "hardware_profile", Message: message}})
	for _, body := range []string{`{}`, `{"hardware_profile":"linux"}`} {
		w := apiRequest(h, "POST", "/api/v1/clouds/local/machines/existing/adopt", body, true)
		if w.Code != 422 || !strings.Contains(w.Body.String(), `body.hardware_profile`) {
			t.Fatal("adoption rejection lost correction field", w.Code, w.Body)
		}
		if body != `{}` && !strings.Contains(w.Body.String(), message) {
			t.Fatal("adoption rejection hid compatibility explanation", w.Body)
		}
	}
	page, err := service.List(tasks.Filter{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("rejected adoption created a task", page, err)
	}
}

func TestUnadoptedMutationExplainsRequiredAction(t *testing.T) {
	h, service := policyRejectionAPI(t, &policyRejectionBackend{})
	w := apiRequest(h, "POST", "/api/v1/clouds/local/machines/existing/actions", `{"action":"start"}`, true)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "adopt") || !strings.Contains(w.Body.String(), "hardware profile") {
		t.Fatal("read-only mutation rejection hid adoption guidance", w.Code, w.Body)
	}
	page, err := service.List(tasks.Filter{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("read-only mutation created a task", page, err)
	}
}

func TestDiskPolicyMismatchIsAFieldErrorBeforeAcceptance(t *testing.T) {
	message := "Requested bus conflicts with the applied profile; omit bus to use its ordinary-disk policy."
	h, service := policyRejectionAPI(t, &policyRejectionBackend{managed: true, diskErr: &compute.ValidationError{Field: "bus", Message: message}})
	id := (compute.VolumeAddress{Host: "node", Path: "/disk"}).ID()
	w := apiRequest(h, "POST", "/api/v1/clouds/local/machines/existing/disks/attach", `{"volume_id":"`+id+`","bus":"ide"}`, true)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "body.bus") || !strings.Contains(w.Body.String(), message) {
		t.Fatal(w.Code, w.Body)
	}
	page, err := service.List(tasks.Filter{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("mismatched attachment created task", page, err)
	}
}

func TestAdoptionInfrastructureFailuresDoNotLeakBackendDetails(t *testing.T) {
	h, _ := policyRejectionAPI(t, &policyRejectionBackend{adoptionErr: fmt.Errorf("%w: private connection credential /private/socket", compute.ErrUnavailable)})
	w := apiRequest(h, "POST", "/api/v1/clouds/local/machines/existing/adopt", `{"hardware_profile":"linux"}`, true)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("backend failure was exposed as validation", w.Code, w.Body)
	}
}

func TestAppliedHardwareProjectionDoesNotExposePrivateBackendBindings(t *testing.T) {
	plan, err := hardware.Resolve(hardware.Profile{ID: "linux"}, "/private/emulator")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	vm := vmDTO(&compute.VirtualMachine{AppliedPolicy: raw, Memory: compute.NewSize(1, compute.SizeUnitG)})
	if vm.LibvirtHardware == nil || vm.LibvirtHardware.Profile.ID != "linux" {
		t.Fatal("missing applied profile")
	}
	data, _ := json.Marshal(vm)
	if strings.Contains(string(data), "/private/emulator") {
		t.Fatal("private plan binding leaked")
	}
}
