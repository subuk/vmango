package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gorilla/mux"
	"io"
	"net/http"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
)

type namespaceContextKey struct{}

func requestNamespace(ctx context.Context) compute.NamespaceID {
	id, _ := ctx.Value(namespaceContextKey{}).(compute.NamespaceID)
	return id
}
func workloadPath(path string) bool {
	path = strings.TrimPrefix(path, "/api/v1")
	for _, prefix := range []string{"/clouds", "/keys", "/cloud-init-templates", "/tasks"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return strings.HasPrefix(path, "/namespaces/")
}
func (e *Environ) namespaceGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !workloadPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		values := r.Header.Values("X-Vmango-Namespace")
		if len(values) != 1 || !compute.ValidCatalogID(values[0]) {
			writeAPIProblem(w, 400, "Exactly one valid X-Vmango-Namespace header is required.")
			return
		}
		for _, key := range []string{"namespace", "namespace_id", "context"} {
			if _, ok := r.URL.Query()[key]; ok {
				writeAPIProblem(w, 400, "Workload context is accepted only in X-Vmango-Namespace.")
				return
			}
		}
		if r.Body != nil && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			body, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
			if err != nil || len(body) > 2<<20 {
				writeAPIProblem(w, 400, "Invalid request body.")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var fields map[string]json.RawMessage
			if json.Unmarshal(body, &fields) == nil {
				for _, key := range []string{"namespace", "namespace_id", "context"} {
					if _, ok := fields[key]; ok {
						writeAPIProblem(w, 400, "Workload context is accepted only in X-Vmango-Namespace.")
						return
					}
				}
			}
		}
		id := compute.NamespaceID(values[0])
		if _, err := e.namespaces.Get(id); err != nil {
			writeAPIProblem(w, 404, "Namespace not found.")
			return
		}
		vars := mux.Vars(r)
		if ns := vars["namespace"]; ns != "" && ns != string(id) {
			writeAPIProblem(w, 404, "Namespace not found.")
			return
		}
		if cloud := vars["cloud"]; cloud != "" {
			if err := e.namespaces.Admit(id, cloud); err != nil {
				writeAPIProblem(w, 404, "Cloud not found.")
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/clouds") && e.workloads == nil {
			writeAPIProblem(w, 503, "Workload membership service is unavailable.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), namespaceContextKey{}, id)))
	})
}
func (e *Environ) getWorkloadMachine(ctx context.Context, cloud, id string) (*compute.VirtualMachine, error) {
	if e.workloads == nil {
		return nil, compute.ErrUnavailable
	}
	return e.workloads.Machine(compute.WorkloadContext{Namespace: requestNamespace(ctx), Cloud: cloud}, id)
}
func (e *Environ) getWorkloadVolume(ctx context.Context, cloud, id string) (*compute.Volume, error) {
	if e.workloads == nil {
		return nil, compute.ErrUnavailable
	}
	return e.workloads.Volume(compute.WorkloadContext{Namespace: requestNamespace(ctx), Cloud: cloud}, id)
}

type apiNamespaceList struct {
	Items []compute.Namespace `json:"items" nullable:"false"`
}
type apiNamespaceInput struct {
	Namespace string `path:"namespace"`
}

func (e *Environ) registerNamespaces(api huma.API) {
	registerAPI(api, "GET", "/namespaces", "list-namespaces", "Discover configured workload namespaces", func(ctx context.Context, in *apiEmpty) (*apiResult[apiNamespaceList], error) {
		return &apiResult[apiNamespaceList]{Body: apiNamespaceList{Items: e.namespaces.List()}}, nil
	})
	registerAPI(api, "GET", "/namespaces/{namespace}", "get-namespace", "Read selected namespace", func(ctx context.Context, in *apiNamespaceInput) (*apiResult[compute.Namespace], error) {
		n, err := e.namespaces.Get(compute.NamespaceID(in.Namespace))
		return &apiResult[compute.Namespace]{Body: n}, apiError(err)
	})
	registerAPI(api, "GET", "/administration/configuration/namespaces", "administration-namespaces", "Inspect configured namespaces", func(ctx context.Context, in *apiEmpty) (*apiResult[apiNamespaceList], error) {
		return &apiResult[apiNamespaceList]{Body: apiNamespaceList{Items: e.namespaces.List()}}, nil
	})
	registerAPI(api, "GET", "/administration/configuration/namespaces/{namespace}", "administration-namespace", "Inspect namespace configuration", func(ctx context.Context, in *apiNamespaceInput) (*apiResult[compute.Namespace], error) {
		n, err := e.namespaces.Get(compute.NamespaceID(in.Namespace))
		return &apiResult[compute.Namespace]{Body: n}, apiError(err)
	})
	registerAPI(api, "GET", "/administration/tasks", "administration-tasks", "Inspect task history across namespaces", func(ctx context.Context, in *apiTaskListInput) (*apiResult[tasks.Page], error) {
		f, err := taskFilter(in)
		if err != nil {
			return nil, err
		}
		page, err := e.tasks.List(f)
		return &apiResult[tasks.Page]{Body: page}, taskError(err)
	})
	registerAPI(api, "GET", "/administration/tasks/{task}", "administration-task", "Inspect a task across namespaces", func(ctx context.Context, in *apiTaskInput) (*apiResult[tasks.Task], error) {
		t, err := e.tasks.Get(in.Task)
		return &apiResult[tasks.Task]{Body: t}, taskError(err)
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/machines", "administration-machines", "Inspect cloud machines across namespaces", func(ctx context.Context, in *apiCloudInput) (*apiResult[apiMachineList], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		list, err := e.clouds.ListMachines(ctx, in.Cloud)
		if err != nil {
			return nil, apiError(err)
		}
		out := apiMachineList{Items: []apiVM{}, UnavailableHosts: list.UnavailableHosts}
		for _, vm := range list.Machines {
			out.Items = append(out.Items, e.machineDTO(vm))
		}
		return &apiResult[apiMachineList]{Body: out}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/machines/{vm}", "administration-machine", "Inspect machine across namespaces", func(ctx context.Context, in *apiVMInput) (*apiResult[apiVMDetail], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		vm, err := e.clouds.GetMachine(ctx, in.Cloud, in.VM)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiVMDetail]{Body: apiVMDetail{APIVMFields: e.machineDTO(vm)}}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/volumes", "administration-volumes", "Inspect cloud volumes across namespaces", func(ctx context.Context, in *apiCloudVolumesInput) (*apiResult[apiVolumeList], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		if e.workloads == nil {
			return nil, apiError(compute.ErrUnavailable)
		}
		list, err := e.clouds.ListVolumes(in.Cloud, false, in.Host)
		if err != nil {
			return nil, apiError(err)
		}
		out := apiVolumeList{Items: []apiVolume{}, UnavailableHosts: list.UnavailableHosts}
		for _, v := range list.Volumes {
			if e.workloads != nil {
				if err = e.workloads.ReadVolume(v); err != nil {
					return nil, apiError(err)
				}
			}
			out.Items = append(out.Items, e.volumeDTO(v))
		}
		return &apiResult[apiVolumeList]{Body: out}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/volumes/{volume}", "administration-volume", "Inspect volume across namespaces", func(ctx context.Context, in *apiVolumeInput) (*apiResult[apiVolumeDetail], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		if e.workloads == nil {
			return nil, apiError(compute.ErrUnavailable)
		}
		v, err := e.clouds.GetVolume(in.Cloud, in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		if e.workloads != nil {
			if err = e.workloads.ReadVolume(v); err != nil {
				return nil, apiError(err)
			}
		}
		return &apiResult[apiVolumeDetail]{Body: apiVolumeDetail{APIVolumeFields: e.volumeDTO(v)}}, nil
	})
}

type apiAssignmentPreview struct {
	Machines []apiVM     `json:"machines" nullable:"false"`
	Volumes  []apiVolume `json:"volumes" nullable:"false"`
}

func (e *Environ) assignmentPreview(ctx context.Context, cloud string, ids []string, disks []compute.LocalDiskRef) (*apiResult[apiAssignmentPreview], error) {
	if e.workloads == nil {
		return nil, apiError(compute.ErrUnavailable)
	}
	p, err := e.workloads.PreviewAssignment(compute.WorkloadContext{Namespace: requestNamespace(ctx), Cloud: cloud}, ids, disks)
	if err != nil {
		return nil, apiError(err)
	}
	out := apiAssignmentPreview{Machines: []apiVM{}, Volumes: []apiVolume{}}
	for _, vm := range p.Machines {
		out.Machines = append(out.Machines, e.machineDTO(vm))
	}
	for _, v := range p.Volumes {
		out.Volumes = append(out.Volumes, e.volumeDTO(v))
	}
	return &apiResult[apiAssignmentPreview]{Body: out}, nil
}
func (e *Environ) registerAssignments(api huma.API) {
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{vm}/assign/preview", "preview-machine-assignment", "Preview complete namespace assignment graph", func(ctx context.Context, in *apiVMInput) (*apiResult[apiAssignmentPreview], error) {
		return e.assignmentPreview(ctx, in.Cloud, []string{in.VM}, nil)
	})
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{vm}/assign", "assign-machine", "Assign an unassigned machine graph to selected namespace", func(ctx context.Context, in *apiCloudMachineInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.assign", compute.InfrastructureCommand{CloudID: in.Cloud, VM: in.VM})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/volumes/{volume}/assign/preview", "preview-volume-assignment", "Preview namespace assignment for a detached volume", func(ctx context.Context, in *apiVolumeInput) (*apiResult[apiAssignmentPreview], error) {
		v, err := e.clouds.VolumeAddress(in.Cloud, in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return e.assignmentPreview(ctx, in.Cloud, nil, []compute.LocalDiskRef{{Host: v.Host, Path: v.Path}})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/volumes/{volume}/assign", "assign-volume", "Assign an unassigned detached volume to selected namespace", func(ctx context.Context, in *apiVolumeDeleteInput) (*apiAccepted, error) {
		v, err := e.clouds.VolumeAddress(in.Cloud, in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "volume.assign", compute.InfrastructureCommand{CloudID: in.Cloud, Node: v.Host, Path: v.Path})
	})
}
