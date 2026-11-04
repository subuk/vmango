package api

import (
	"github.com/rs/zerolog"
	"net/http"
	"subuk/vmango/auth"
	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/tasks"
)

func testNamespaceRegistry() *compute.NamespaceService {
	registry, err := compute.NewNamespaceService([]compute.Namespace{{ID: "test", Clouds: []string{"local", "a", "b", "c", "one", "two", "first", "second", "edge", "other", "node", "remote", "cloud", "cloud-a", "cloud-b", "unavailable", "missing"}}})
	if err != nil {
		panic(err)
	}
	return registry
}

// Existing endpoint fixtures explicitly select a namespace. Membership tests
// supply authoritative repositories and a WorkloadService separately.
func newScopedTestAPI(cfg *config.Config, logger zerolog.Logger, networks *compute.NetworkService, keys *compute.KeyService, pools *compute.VolumePoolService, nodes *compute.NodeService, volumes *compute.VolumeService, vms *compute.VirtualMachineService, manager *compute.VirtualMachineManager, images *compute.ImageService, authentication *auth.Service, taskService *tasks.Service, services ...Services) (http.Handler, error) {
	var opts Services
	if len(services) > 0 {
		opts = services[0]
	}
	if opts.Namespaces == nil {
		opts.Namespaces = testNamespaceRegistry()
		if manager != nil && manager.Clouds != nil && len(manager.Clouds.Definitions) > 0 {
			ids := []string{}
			for _, c := range manager.Clouds.Definitions {
				ids = append(ids, c.ID)
			}
			var err error
			opts.Namespaces, err = compute.NewNamespaceService([]compute.Namespace{{ID: "test", Clouds: ids}})
			if err != nil {
				return nil, err
			}
		}
	}
	if opts.Workloads == nil && manager != nil && manager.Clouds != nil {
		if vms != nil {
			if _, ok := vms.VirtualMachineRepository.(*namespaceFixtureVMRepository); !ok {
				vms.VirtualMachineRepository = &namespaceFixtureVMRepository{VirtualMachineRepository: vms.VirtualMachineRepository}
			}
		}
		manager.Clouds.VMs = vms
		if volumes != nil {
			manager.Clouds.Volumes = volumes
		}
		opts.Workloads = &compute.WorkloadService{Namespaces: opts.Namespaces, Clouds: manager.Clouds, Assignments: namespaceFixtureAssignments{}}
	}
	return New(cfg, logger, networks, keys, pools, nodes, volumes, vms, manager, images, authentication, taskService, opts)
}

type namespaceFixtureVMRepository struct {
	compute.VirtualMachineRepository
}

func (r *namespaceFixtureVMRepository) Get(id, node string) (*compute.VirtualMachine, error) {
	v, err := r.VirtualMachineRepository.Get(id, node)
	if v != nil {
		v.NamespaceID = "test"
	}
	return v, err
}
func (r *namespaceFixtureVMRepository) List(o compute.VirtualMachineListOptions) ([]*compute.VirtualMachine, error) {
	vs, err := r.VirtualMachineRepository.List(o)
	for _, v := range vs {
		v.NamespaceID = "test"
	}
	return vs, err
}

type namespaceFixtureAssignments struct{}

func (namespaceFixtureAssignments) Get(v *compute.Volume, scope string) (*compute.VolumeAssignment, error) {
	return &compute.VolumeAssignment{NamespaceID: "test", Label: v.Name}, nil
}
func (namespaceFixtureAssignments) Put(v *compute.Volume, scope string, a compute.VolumeAssignment) error {
	return nil
}
func (namespaceFixtureAssignments) Delete(v *compute.Volume, scope string) error { return nil }
