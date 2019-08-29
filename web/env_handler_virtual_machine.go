package web

import (
	"fmt"
	"net/http"
	"strconv"
	"subuk/vmango/compute"

	"github.com/gorilla/mux"
)

func (env *Environ) VirtualMachineList(rw http.ResponseWriter, req *http.Request) {
	vms, err := env.compute.VirtualMachineList()
	if err != nil {
		env.error(rw, req, err, "vm list failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title string
		Vms   []*compute.VirtualMachine
		User  *User
	}{"Virtual Machines", vms, env.Session(req).AuthUser()}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineDetail(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"])

	if err != nil {
		env.error(rw, req, err, "vm get failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title string
		Vm    *compute.VirtualMachine
		User  *User
	}{"Virtual Machine", vm, env.Session(req).AuthUser()}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/detail", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineStateSetFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	action := urlvars["action"]
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"])
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Action  string
		Vm      *compute.VirtualMachine
		User    *User
		Request *http.Request
	}{"Set Machine State", action, vm, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/setstate", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineStateSetFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := env.compute.VirtualMachineAction(urlvars["id"], urlvars["action"]); err != nil {
		http.Error(rw, fmt.Sprintf("failed to %s machine: %s", urlvars["action"], err), http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"])
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineAddFormShow(rw http.ResponseWriter, req *http.Request) {
	context, err := env.compute.VirtualMachineCreateContext()
	if err != nil {
		env.error(rw, req, err, "cannot fetch vm create context", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Context compute.VirtualMachineCreateContext
		User    *User
		Request *http.Request
	}{"Create Virtual Machine", context, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/add", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineAddFormProcess(rw http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	vcpus, err := strconv.ParseInt(req.Form.Get("Vcpus"), 10, 16)
	if err != nil {
		http.Error(rw, "invalid vcpus value: "+err.Error(), http.StatusBadRequest)
	}
	memoryMb, err := strconv.ParseUint(req.Form.Get("MemoryMb"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid memoryMb value: "+err.Error(), http.StatusBadRequest)
	}
	rootVolumeSizeGb, err := strconv.ParseUint(req.Form.Get("RootVolumeSizeGb"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid root volume size: "+err.Error(), http.StatusBadRequest)
	}
	rootVolumeParams := compute.VirtualMachineCreateParamsVolume{
		Name: req.Form.Get("RootVolumeName"), Pool: req.Form.Get("RootVolumePool"),
		CloneFrom:  req.Form.Get("RootVolumeSource"),
		DeviceType: compute.DeviceTypeDisk.String(),
		Format:     req.Form.Get("RootVolumeFormat"), SizeMb: rootVolumeSizeGb * 1024,
	}
	mainInterface := compute.VirtualMachineCreateParamsInterface{
		Network: req.Form.Get("Network"),
		Mac:     req.Form.Get("Mac"),
	}
	params := compute.VirtualMachineCreateParams{
		Id:         req.Form.Get("Name"),
		VCpus:      int(vcpus),
		Arch:       req.Form.Get("Arch"),
		MemoryKb:   uint(memoryMb) * 1024,
		Volumes:    []compute.VirtualMachineCreateParamsVolume{rootVolumeParams},
		Interfaces: []compute.VirtualMachineCreateParamsInterface{mainInterface},
		Config:     compute.VirtualMachineCreateParamsConfig{},
	}
	vm, err := env.compute.VirtualMachineCreate(params)
	if err != nil {
		env.error(rw, req, err, "cannot fetch create vm", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", vm.Id)
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineDeleteFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"])
	if err != nil {
		env.error(rw, req, err, "virtual-machine get failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Vm      *compute.VirtualMachine
		User    *User
		Request *http.Request
	}{"Delete VirtualMachine", vm, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/delete", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineDeleteFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := env.compute.VirtualMachineDelete(urlvars["id"]); err != nil {
		env.error(rw, req, err, "cannot delete virtual machine", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}
