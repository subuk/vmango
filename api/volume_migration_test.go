package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"

	"github.com/rs/zerolog"
)

type volumeMigrationRepo struct {
	compute.VolumeRepository
	reads    int
	failed   map[string]bool
	missing  bool
	attached bool
	hidden   bool
}

func (r *volumeMigrationRepo) Get(path, node string) (*compute.Volume, error) {
	r.reads++
	if r.failed[node] {
		return nil, compute.ErrUnavailable
	}
	if r.missing {
		return nil, compute.ErrVolumeNotFound
	}
	v := &compute.Volume{Path: path, NodeId: node, Name: "same", Pool: "pool", Size: compute.NewSize(1, compute.SizeUnitG), Format: compute.VolumeFormatQcow2}
	if r.attached {
		v.AttachedTo = "vm"
	}
	v.Metadata.Hidden = r.hidden
	return v, nil
}
func (r *volumeMigrationRepo) List(options compute.VolumeListOptions) ([]*compute.Volume, error) {
	v, err := r.Get("/same", options.NodeIds[0])
	if err != nil {
		return nil, err
	}
	return []*compute.Volume{v}, nil
}

type volumeMigrationPools struct {
	compute.VolumePoolRepository
	fail  bool
	reads int
}

func (r *volumeMigrationPools) List(options compute.VolumePoolListOptions) ([]*compute.VolumePool, error) {
	r.reads++
	if r.fail {
		return nil, compute.ErrUnavailable
	}
	return []*compute.VolumePool{{NodeId: options.NodeIds[0], Name: "pool"}}, nil
}
func volumeMigrationAPI(t *testing.T, repo *volumeMigrationRepo, pools *volumeMigrationPools) http.Handler {
	t.Helper()
	volumes := compute.NewVolumeService(repo)
	poolService := compute.NewVolumePoolService(pools)
	clouds := &compute.CloudService{Definitions: []compute.Cloud{{ID: "one", Hosts: []compute.CloudHost{{Node: "a", Zone: "zone", Storage: map[string]compute.StorageBinding{"standard": {Pool: "pool", Format: compute.VolumeFormatQcow2}}}, {Node: "b", Zone: "zone"}}, StorageClasses: []compute.CatalogEntry{{ID: "standard", Name: "Standard"}}}, {ID: "two", Hosts: []compute.CloudHost{{Node: "c"}}}}, Volumes: volumes, Pools: poolService}
	return newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, poolService, nil, volumes, nil, &compute.VirtualMachineManager{Clouds: clouds}, nil)
}
func TestFlatVolumeAPIAndDetailCatalog(t *testing.T) {
	repo := &volumeMigrationRepo{failed: map[string]bool{"b": true}}
	pools := &volumeMigrationPools{}
	h := volumeMigrationAPI(t, repo, pools)
	w := apiRequest(h, "GET", "/api/v1/clouds/one/volumes", "", true)
	var list apiVolumeList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	id := (compute.VolumeAddress{Host: "a", Path: "/same"}).ID()
	if w.Code != 200 || len(list.Items) != 1 || list.Items[0].ID != id || !reflect.DeepEqual(list.UnavailableHosts, []string{"b"}) {
		t.Fatalf("invalid flat list: %d %s", w.Code, w.Body)
	}
	if pools.reads != 0 {
		t.Fatal("list discovered catalogs per row")
	}
	if strings.Contains(w.Body.String(), "catalog") {
		t.Fatal("list row contains detail catalog")
	}
	repo.reads = 0
	w = apiRequest(h, "GET", "/api/v1/clouds/one/volumes/"+id, "", true)
	var detail apiVolumeDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || detail.ID != id || detail.Catalog == nil || len(detail.Catalog.StorageClasses) != 1 || repo.reads != 1 {
		t.Fatalf("detail lost catalog or looked up volume twice: %d %s reads=%d", w.Code, w.Body, repo.reads)
	}
	pools.fail = true
	w = apiRequest(h, "GET", "/api/v1/clouds/one/volumes/"+id, "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"catalog":null`) || !strings.Contains(w.Body.String(), `"id":"`+id+`"`) {
		t.Fatal("supplemental failure hid resource", w.Code, w.Body)
	}
	pools.fail = false
	repo.attached = true
	w = apiRequest(h, "GET", "/api/v1/clouds/one/volumes/"+id, "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"storage_classes":[]`) {
		t.Fatal("attached clone source retained choices", w.Code, w.Body)
	}
	repo.failed["a"] = true
	w = apiRequest(h, "GET", "/api/v1/clouds/one/volumes", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"unavailable_hosts":["a","b"]`) {
		t.Fatal("all unavailable appeared empty", w.Code, w.Body)
	}
}
func TestFlatVolumeAPIRejectsMalformedAndWrongCloudBeforeStorage(t *testing.T) {
	repo := &volumeMigrationRepo{failed: map[string]bool{}}
	h := volumeMigrationAPI(t, repo, &volumeMigrationPools{})
	wrong := (compute.VolumeAddress{Host: "c", Path: "/same"}).ID()
	for _, test := range []struct {
		path   string
		status int
	}{{"/api/v1/clouds/one/volumes/" + wrong, 404}, {"/api/v1/clouds/one/volumes/b2xkLXBhdGg", 422}, {"/api/v1/clouds/one/volumes/YQ.YR", 422}, {"/api/v1/clouds/one/volumes/a/L3NhbWU", 404}} {
		w := apiRequest(h, "GET", test.path, "", true)
		if w.Code != test.status {
			t.Fatalf("%s: %d %s", test.path, w.Code, w.Body)
		}
	}
	if repo.reads != 0 {
		t.Fatal("invalid address reached storage", repo.reads)
	}
	id := (compute.VolumeAddress{Host: "a", Path: "/same"}).ID()
	repo.missing = true
	if w := apiRequest(h, "GET", "/api/v1/clouds/one/volumes/"+id, "", true); w.Code != 404 {
		t.Fatal("missing volume not 404", w.Code, w.Body)
	}
	repo.missing = false
	repo.failed["a"] = true
	if w := apiRequest(h, "GET", "/api/v1/clouds/one/volumes/"+id, "", true); w.Code != 503 {
		t.Fatal("unavailable volume not 503", w.Code, w.Body)
	}
}
func TestExistingDiskInputTranslatesToUnchangedDurableAddress(t *testing.T) {
	repo := &volumeMigrationRepo{}
	clouds := &compute.CloudService{Definitions: []compute.Cloud{{ID: "one", Hosts: []compute.CloudHost{{Node: "a"}}}}, Volumes: compute.NewVolumeService(repo)}
	input := apiMachineCreate{Name: "machine", ExistingDisks: []apiExistingDisk{{VolumeID: (compute.VolumeAddress{Host: "a", Path: "/same"}).ID()}}}
	spec, err := input.domain("one", clouds)
	if err != nil || !reflect.DeepEqual(spec.ExistingDisks, []compute.LocalDiskRef{{Host: "a", Path: "/same"}}) {
		t.Fatal("durable path changed", spec, err)
	}
	raw, err := json.Marshal(spec)
	if err != nil || !strings.Contains(string(raw), `"host":"a"`) || !strings.Contains(string(raw), `"volume_id":"/same"`) {
		t.Fatal("durable shape changed", string(raw), err)
	}
	raw, err = json.Marshal(input)
	if err != nil || strings.Contains(string(raw), `"host"`) {
		t.Fatal("public shape retained host", string(raw), err)
	}
}

func TestResourceDetailSchemasContainResourceAndCatalogFields(t *testing.T) {
	h := volumeMigrationAPI(t, &volumeMigrationRepo{}, &volumeMigrationPools{})
	w := apiRequest(h, "GET", "/api/openapi.json", "", true)
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for name, required := range map[string][]string{"ApiVMDetail": {"id", "cloud", "node", "disks", "catalog"}, "ApiVolumeDetail": {"id", "cloud", "node", "size_bytes", "catalog"}} {
		for _, field := range required {
			if _, ok := spec.Components.Schemas[name].Properties[field]; !ok {
				t.Fatalf("%s schema omits %s", name, field)
			}
		}
	}
	public := spec.Components.Schemas["ApiExistingDisk"].Properties
	if _, ok := public["host"]; ok {
		t.Fatal("public existing disk schema retained host")
	}
}

type volumeMachineRepo struct {
	compute.VirtualMachineRepository
	gets int
}

func (r *volumeMachineRepo) List(options compute.VirtualMachineListOptions) ([]*compute.VirtualMachine, error) {
	if options.NodeIds[0] != "a" {
		return []*compute.VirtualMachine{}, nil
	}
	return []*compute.VirtualMachine{{Id: "vm", NodeId: "a", Name: "machine", Memory: compute.NewSize(1, compute.SizeUnitG)}}, nil
}
func (r *volumeMachineRepo) Get(id, node string) (*compute.VirtualMachine, error) {
	r.gets++
	return &compute.VirtualMachine{Id: id, NodeId: node, Name: "machine", Memory: compute.NewSize(1, compute.SizeUnitG)}, nil
}
func TestMachineDiskAddressCannotSelectVolumeOnAnotherHost(t *testing.T) {
	repo := &volumeMigrationRepo{}
	volumes := compute.NewVolumeService(repo)
	vms := compute.NewVirtualMachineService(&volumeMachineRepo{})
	clouds := &compute.CloudService{Definitions: []compute.Cloud{{ID: "one", Hosts: []compute.CloudHost{{Node: "a"}, {Node: "b"}}}, {ID: "two", Hosts: []compute.CloudHost{{Node: "c"}}}}, Volumes: volumes, VMs: vms}
	h := newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, nil, nil, volumes, vms, &compute.VirtualMachineManager{Clouds: clouds}, nil)
	for _, node := range []string{"b", "c"} {
		id := (compute.VolumeAddress{Host: node, Path: "/same"}).ID()
		want := 422
		if node == "c" {
			want = 404
		}
		for _, method := range []string{"POST", "DELETE"} {
			path := "/api/v1/clouds/one/machines/vm/disks/" + id
			body := ""
			if method == "POST" {
				path = "/api/v1/clouds/one/machines/vm/disks/attach"
				body = `{"volume_id":"` + id + `"}`
			}
			w := apiRequest(h, method, path, body, true)
			if w.Code != want {
				t.Fatalf("%s disk on %s: %d %s", method, node, w.Code, w.Body)
			}
		}
	}
	if repo.reads != 0 {
		t.Fatal("cross-host attachment touched storage", repo.reads)
	}
}
func TestMachineDetailCatalogReusesResolvedMachine(t *testing.T) {
	repo := &volumeMachineRepo{}
	vms := compute.NewVirtualMachineService(repo)
	pools := &volumeMigrationPools{}
	clouds := &compute.CloudService{Definitions: []compute.Cloud{{ID: "one", Hosts: []compute.CloudHost{{Node: "a"}}}}, VMs: vms, Pools: compute.NewVolumePoolService(pools)}
	h := newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, clouds.Pools, nil, nil, vms, &compute.VirtualMachineManager{Clouds: clouds}, nil)
	w := apiRequest(h, "GET", "/api/v1/clouds/one/machines/vm", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"catalog":{"networks":[],"storage_classes":[]}`) || repo.gets != 1 {
		t.Fatal("machine detail failed or repeated resource lookup", w.Code, w.Body, repo.gets)
	}
	pools.fail = true
	w = apiRequest(h, "GET", "/api/v1/clouds/one/machines/vm", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"catalog":null`) {
		t.Fatal("supplemental failure blocked readable machine", w.Code, w.Body)
	}
}

func TestHiddenVolumeDetailRemainsReadableForExistingDiskLabels(t *testing.T) {
	repo := &volumeMigrationRepo{hidden: true}
	h := volumeMigrationAPI(t, repo, &volumeMigrationPools{})
	id := (compute.VolumeAddress{Host: "a", Path: "/same"}).ID()
	w := apiRequest(h, "GET", "/api/v1/clouds/one/volumes/"+id, "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"same"`) || !strings.Contains(w.Body.String(), `"storage_classes":[]`) {
		t.Fatal("existing hidden source detail changed visibility or exposed clone choices", w.Code, w.Body)
	}
	w = apiRequest(h, "GET", "/api/v1/clouds/one/volumes", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("hidden source appeared in workload list", w.Code, w.Body)
	}
}

// The task handler deliberately accepts without backend reads: API translation
// must reach task deduplication before any storage or VM discovery can fail.
func TestVolumeSubmissionRecoveryPrecedesBackendResolution(t *testing.T) {
	id := (compute.VolumeAddress{Host: "a", Path: "/same"}).ID()
	other := (compute.VolumeAddress{Host: "b", Path: "/same"}).ID()
	for _, test := range []struct{ name, method, path, body string }{
		{"delete", "DELETE", "/api/v1/clouds/one/volumes/" + id, ""},
		{"clone", "POST", "/api/v1/clouds/one/volumes/" + id + "/clone", `{"name":"copy","storage_class":"standard","size_bytes":1073741824}`},
		{"resize", "POST", "/api/v1/clouds/one/volumes/" + id + "/resize", `{"size_bytes":2147483648}`},
		{"attach", "POST", "/api/v1/clouds/one/machines/vm/disks/attach", `{"volume_id":"` + id + `"}`},
		{"detach", "DELETE", "/api/v1/clouds/one/machines/vm/disks/" + id, ""},
		{"create-existing", "POST", "/api/v1/clouds/one/machines", `{"name":"vm","image":"image","instance_type":"small","zone":"z","network":"lan","storage_class":"standard","start":false,"existing_disks":[{"volume_id":"` + id + `"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := apiTestConfig(t)
			repository, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			service := tasks.New(repository, taskAPIHandler{}, zerolog.Nop())
			backend := &volumeMigrationRepo{}
			volumes := compute.NewVolumeService(backend)
			clouds := &compute.CloudService{Definitions: []compute.Cloud{{ID: "one", Hosts: []compute.CloudHost{{Node: "a"}, {Node: "b"}}}}, Volumes: volumes}
			h, err := New(cfg, zerolog.Nop(), nil, nil, nil, nil, volumes, nil, &compute.VirtualMachineManager{Clouds: clouds}, nil, testAuthService(t, cfg), service)
			if err != nil {
				t.Fatal(err)
			}
			submit := func(path, body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(test.method, "https://vmango.test"+path, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+testWebToken())
				r.Header.Set("Idempotency-Key", "same-submission-key")
				if body != "" {
					r.Header.Set("Content-Type", "application/json")
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			first := submit(test.path, test.body)
			if first.Code != 202 {
				t.Fatal("submission failed", first.Code, first.Body)
			}
			backend.missing = true
			backend.failed = map[string]bool{"a": true}
			retry := submit(test.path, test.body)
			if retry.Code != 202 || retry.Header().Get("Location") != first.Header().Get("Location") || backend.reads != 0 {
				t.Fatal("recovery performed backend reads or lost original task", retry.Code, retry.Body, backend.reads)
			}
			changed := submit(strings.ReplaceAll(test.path, id, other), strings.ReplaceAll(test.body, id, other))
			if changed.Code != 409 {
				t.Fatal("same path on a different host did not conflict on reused key", changed.Code, changed.Body)
			}
		})
	}
}

func TestNewVolumeMutationRejectsMissingOrUnavailableBeforeAcceptance(t *testing.T) {
	id := (compute.VolumeAddress{Host: "a", Path: "/same"}).ID()
	for _, missing := range []bool{true, false} {
		repo := &volumeMigrationRepo{missing: missing, failed: map[string]bool{"a": !missing}}
		h := volumeMigrationAPI(t, repo, &volumeMigrationPools{})
		want := 503
		if missing {
			want = 404
		}
		for _, operation := range []struct{ method, path, body string }{{"DELETE", "/api/v1/clouds/one/volumes/" + id, ""}, {"POST", "/api/v1/clouds/one/volumes/" + id + "/clone", `{"name":"copy","storage_class":"standard","size_bytes":1073741824}`}, {"POST", "/api/v1/clouds/one/volumes/" + id + "/resize", `{"size_bytes":2147483648}`}} {
			w := apiRequest(h, operation.method, operation.path, operation.body, true)
			if w.Code != want || w.Header().Get("Location") != "" {
				t.Fatal("invalid mutation was accepted or changed status", operation.path, w.Code, w.Body)
			}
		}
	}
}
