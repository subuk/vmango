package web

import (
	"fmt"
	"net/http"
	"strconv"
	"subuk/vmango/compute"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
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
	volumes, err := env.compute.VolumeList()
	if err != nil {
		env.error(rw, req, err, "cannot list volumes", http.StatusInternalServerError)
		return
	}
	networks, err := env.compute.NetworkList()
	if err != nil {
		env.error(rw, req, err, "cannot list networks", http.StatusInternalServerError)
		return
	}

	attachedVolumes := []*compute.Volume{}
	availableVolumes := []*compute.Volume{}
	for _, volume := range volumes {
		if attachmentInfo := vm.AttachmentInfo(volume.Path); attachmentInfo != nil {
			attachedVolumes = append(attachedVolumes, volume)
			continue
		}
		if volume.AttachedTo == "" && volume.Format != compute.FormatIso && volume.Metadata.OsName == "" {
			availableVolumes = append(availableVolumes, volume)
			continue
		}
	}
	data := struct {
		Title            string
		Vm               *compute.VirtualMachine
		AttachedVolumes  []*compute.Volume
		AvailableVolumes []*compute.Volume
		Networks         []*compute.Network
		User             *User
		Request          *http.Request
	}{"Virtual Machine", vm, attachedVolumes, availableVolumes, networks, env.Session(req).AuthUser(), req}
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
	data := struct {
		Title    string
		User     *User
		Request  *http.Request
		Volumes  []*compute.Volume
		Images   []*compute.Volume
		Pools    []*compute.VolumePool
		Networks []*compute.Network
		Keys     []*compute.Key
		Arches   []compute.Arch
	}{
		Title:   "Create Virtual Machine",
		Request: req,
		User:    env.Session(req).AuthUser(),
		Arches:  []compute.Arch{compute.ArchAmd64},
	}

	images, err := env.compute.ImageList()
	if err != nil {
		env.error(rw, req, err, "cannot list volumes", http.StatusInternalServerError)
		return
	}
	data.Images = images

	pools, err := env.compute.VolumePoolList()
	if err != nil {
		env.error(rw, req, err, "cannot list pools", http.StatusInternalServerError)
		return
	}
	data.Pools = pools

	keys, err := env.compute.KeyList()
	if err != nil {
		env.error(rw, req, err, "cannot list keys", http.StatusInternalServerError)
		return
	}
	data.Keys = keys

	networks, err := env.compute.NetworkList()
	if err != nil {
		env.error(rw, req, err, "cannot list networks", http.StatusInternalServerError)
		return
	}
	data.Networks = networks

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
		return
	}
	memoryMb, err := strconv.ParseUint(req.Form.Get("MemoryMb"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid memoryMb value: "+err.Error(), http.StatusBadRequest)
		return
	}
	rootVolumeSizeGb, err := strconv.ParseUint(req.Form.Get("RootVolumeSizeGb"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid root volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	var accessVlan uint
	accessVlanRaw := req.Form.Get("AccessVlan")
	if accessVlanRaw != "" {
		parsed, err := strconv.ParseUint(accessVlanRaw, 10, 16)
		if err != nil {
			http.Error(rw, "invalid vlan: "+err.Error(), http.StatusBadRequest)
			return
		}
		accessVlan = uint(parsed)
	}
	rootVolumeParams := compute.VirtualMachineCreateParamsVolume{
		Name: req.Form.Get("RootVolumeName"), Pool: req.Form.Get("RootVolumePool"),
		CloneFrom:  req.Form.Get("RootVolumeSource"),
		DeviceType: compute.DeviceTypeDisk.String(),
		Format:     req.Form.Get("RootVolumeFormat"), SizeMb: rootVolumeSizeGb * 1024,
	}
	mainInterface := compute.VirtualMachineCreateParamsInterface{
		Network:    req.Form.Get("Network"),
		Mac:        req.Form.Get("Mac"),
		AccessVlan: accessVlan,
	}
	params := compute.VirtualMachineCreateParams{
		Id:         req.Form.Get("Name"),
		VCpus:      int(vcpus),
		Arch:       req.Form.Get("Arch"),
		MemoryKb:   uint(memoryMb) * 1024,
		Volumes:    []compute.VirtualMachineCreateParamsVolume{rootVolumeParams},
		Interfaces: []compute.VirtualMachineCreateParamsInterface{mainInterface},
		Config: compute.VirtualMachineCreateParamsConfig{
			Hostname:        req.Form.Get("Name"),
			KeyFingerprints: req.Form["Keys"],
			UserData:        req.Form.Get("Userdata"),
		},
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

func (env *Environ) VirtualMachineAttachDiskFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := env.compute.VirtualMachineAttachVolume(urlvars["id"], req.Form.Get("Path"), compute.DeviceTypeDisk); err != nil {
		env.error(rw, req, err, "cannot attach disk", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"])
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineDetachVolumeFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := env.compute.VirtualMachineDetachVolume(urlvars["id"], req.Form.Get("Path")); err != nil {
		env.error(rw, req, err, "cannot detach disk", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"])
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineAttachInterfaceFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	id := urlvars["id"]
	mac := req.Form.Get("Mac")
	network := req.Form.Get("Network")

	var accessVlan uint
	accessVlanRaw := req.Form.Get("AccessVlan")
	if accessVlanRaw != "" {
		parsed, err := strconv.ParseUint(accessVlanRaw, 10, 16)
		if err != nil {
			http.Error(rw, "invalid vlan: "+err.Error(), http.StatusBadRequest)
			return
		}
		accessVlan = uint(parsed)
	}

	if _, err := env.compute.VirtualMachineAttachInterface(id, network, mac, "virtio", accessVlan); err != nil {
		env.error(rw, req, err, "cannot attach interface", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", id)
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineDetachInterfaceFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := env.compute.VirtualMachineDetachInterface(urlvars["id"], req.Form.Get("Mac")); err != nil {
		env.error(rw, req, err, "cannot detach interface", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"])
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineConsoleShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"])
	if err != nil {
		env.error(rw, req, err, "cannot get vm", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Vm      *compute.VirtualMachine
		User    *User
		Request *http.Request
	}{"Virtual Machine Serial Console", vm, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/console", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineConsoleWS(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)

	wsconn, err := env.ws.Upgrade(rw, req, nil)
	if err != nil {
		env.logger.Debug().Err(err).Msg("cannot upgrade websocket connection")
		return
	}

	console, err := env.compute.VirtualMachineGetConsoleStream(urlvars["id"])
	if err != nil {
		env.error(rw, req, err, "cannot get vm console", http.StatusInternalServerError)
		return
	}
	defer console.Close()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := console.Read(buf)
			if err != nil {
				env.logger.Debug().Err(err).Msg("console read error")
				return
			}
			if err := wsconn.WriteMessage(websocket.TextMessage, buf[0:n]); err != nil {
				env.logger.Debug().Err(err).Msg("wsconn write error")
				return
			}
		}
	}()
	for {
		_, message, err := wsconn.ReadMessage()
		if err != nil {
			env.logger.Debug().Err(err).Msg("ws read error")
			return
		}
		if _, err := console.Write(message); err != nil {
			env.logger.Debug().Err(err).Msg("console write error")
			return
		}
	}
}
