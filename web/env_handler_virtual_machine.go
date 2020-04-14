package web

import (
	"fmt"
	"io"
	"log"
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

var DeviceTypes = []compute.DeviceType{
	compute.DeviceTypeDisk,
	compute.DeviceTypeCdrom,
}

var DeviceBuses = []compute.DeviceBus{
	compute.DeviceBusVirtio,
	compute.DeviceBusScsi,
	compute.DeviceBusIde,
}

var InterfaceModels = []string{
	"virtio",
}

func (env *Environ) VirtualMachineDetail(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"], urlvars["node"])
	if err != nil {
		env.error(rw, req, err, "vm get failed", http.StatusInternalServerError)
		return
	}
	volumes, err := env.compute.VolumeList(compute.VolumeListOptions{NodeId: vm.NodeId})
	if err != nil {
		env.error(rw, req, err, "cannot list volumes", http.StatusInternalServerError)
		return
	}
	networks, err := env.networks.List(compute.NetworkListOptions{NodeId: vm.NodeId})
	if err != nil {
		env.error(rw, req, err, "cannot list networks", http.StatusInternalServerError)
		return
	}

	attachedVolumes := map[string]*compute.Volume{}
	availableVolumes := []*compute.Volume{}
	for _, volume := range volumes {
		if attachmentInfo := vm.AttachmentInfo(volume.Path); attachmentInfo != nil {
			attachedVolumes[attachmentInfo.Path] = volume
			continue
		}
		if volume.AttachedTo == "" && volume.Metadata.OsName == "" {
			availableVolumes = append(availableVolumes, volume)
			continue
		}
	}
	data := struct {
		Title            string
		Vm               *compute.VirtualMachine
		AttachedVolumes  map[string]*compute.Volume
		AvailableVolumes []*compute.Volume
		DeviceTypes      []compute.DeviceType
		DeviceBuses      []compute.DeviceBus
		InterfaceModels  []string
		Networks         []*compute.Network
		ActiveTab        string
		User             *User
		Request          *http.Request
	}{"Virtual Machine", vm, attachedVolumes, availableVolumes, DeviceTypes, DeviceBuses, InterfaceModels, networks, req.URL.Query().Get("tab"), env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/detail", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineStateSetFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	action := urlvars["action"]
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"], urlvars["node"])
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
	env.logger.Debug().
		Str("method", req.Method).
		Msg("Requesting state change")
	urlvars := mux.Vars(req)
	if err := env.compute.VirtualMachineAction(urlvars["id"], urlvars["node"], urlvars["action"]); err != nil {
		http.Error(rw, fmt.Sprintf("failed to %s machine: %s", urlvars["action"], err), http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"], "node", urlvars["node"])
	env.logger.Debug().
		Str("url", redirectUrl.Path).
		Msg("Redirecting to new url after action")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineAddFormShow(rw http.ResponseWriter, req *http.Request) {
	data := struct {
		Title    string
		User     *User
		Request  *http.Request
		NodeId   string
		Nodes    []*compute.Node
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

	nodes, err := env.nodes.List()
	if err != nil {
		env.error(rw, req, err, "cannot list networks", http.StatusInternalServerError)
		return
	}
	data.Nodes = nodes

	var selectedNode *compute.Node
	selectedNodeId := req.URL.Query().Get("node")
	for _, node := range nodes {
		if node.Id == selectedNodeId {
			selectedNode = node
			break
		}
	}
	if selectedNode == nil {
		selectedNode = nodes[0]
	}
	data.NodeId = selectedNode.Id

	images, err := env.compute.ImageList(compute.VolumeListOptions{NodeId: selectedNode.Id})
	if err != nil {
		env.error(rw, req, err, "cannot list volumes", http.StatusInternalServerError)
		return
	}
	data.Images = images

	pools, err := env.volpools.List(compute.VolumePoolListOptions{NodeId: selectedNode.Id})
	if err != nil {
		env.error(rw, req, err, "cannot list pools", http.StatusInternalServerError)
		return
	}
	data.Pools = pools

	keys, err := env.keys.List()
	if err != nil {
		env.error(rw, req, err, "cannot list keys", http.StatusInternalServerError)
		return
	}
	data.Keys = keys

	networks, err := env.networks.List(compute.NetworkListOptions{NodeId: selectedNode.Id})
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
	memoryValue, err := strconv.ParseUint(req.Form.Get("MemoryValue"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid memory size: "+req.Form.Get("MemoryValue"), http.StatusBadRequest)
		return
	}
	memoryUnit := compute.NewSizeUnit(req.Form.Get("MemoryUnit"))
	if memoryUnit == compute.SizeUnitUnknown {
		http.Error(rw, "unknown memory size unit: "+req.Form.Get("MemoryUnit"), http.StatusBadRequest)
		return
	}

	rootVolumeSizeValue, err := strconv.ParseUint(req.Form.Get("RootVolumeSizeValue"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid root volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	rootVolumeSizeUnit := compute.NewSizeUnit(req.Form.Get("RootVolumeSizeUnit"))
	if rootVolumeSizeUnit == compute.SizeUnitUnknown {
		http.Error(rw, "unknown root volume size unit: "+req.Form.Get("RootVolumeSizeUnit"), http.StatusBadRequest)
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
		Name:      req.Form.Get("RootVolumeName"),
		Pool:      req.Form.Get("RootVolumePool"),
		CloneFrom: req.Form.Get("RootVolumeSource"),
		Format:    compute.NewVolumeFormat(req.Form.Get("RootVolumeFormat")),
		Size:      compute.NewSize(rootVolumeSizeValue, rootVolumeSizeUnit),
	}
	mainInterface := compute.VirtualMachineCreateParamsInterface{
		Network:    req.Form.Get("Network"),
		Mac:        req.Form.Get("Mac"),
		AccessVlan: accessVlan,
	}
	params := compute.VirtualMachineCreateParams{
		Id:         req.Form.Get("Name"),
		NodeId:     req.Form.Get("NodeId"),
		VCpus:      int(vcpus),
		Arch:       req.Form.Get("Arch"),
		Memory:     compute.NewSize(memoryValue, memoryUnit),
		Start:      req.Form.Get("Start") == "true",
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
		env.error(rw, req, err, "cannot create vm", http.StatusInternalServerError)
		return
	}

	redirectPath := ""
	if params.Start {
		redirectPath = env.url("virtual-machine-console-show", "id", vm.Id, "node", vm.NodeId).Path
	} else {
		redirectPath = env.url("virtual-machine-detail", "id", vm.Id, "node", vm.NodeId).Path
	}
	http.Redirect(rw, req, redirectPath, http.StatusFound)
}

func (env *Environ) VirtualMachineDeleteFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"], urlvars["node"])
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
	deleteVolumes := req.FormValue("DeleteVolumes") == "true"
	if err := env.compute.VirtualMachineDelete(urlvars["id"], urlvars["node"], deleteVolumes); err != nil {
		env.error(rw, req, err, "cannot delete virtual machine", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

var GraphicTypes = []compute.GraphicType{
	compute.GraphicTypeNone,
	compute.GraphicTypeVnc,
	compute.GraphicTypeSpice,
}

func (env *Environ) VirtualMachineUpdateFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"], urlvars["node"])
	if err != nil {
		env.error(rw, req, err, "virtual-machine detail failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title        string
		Vm           *compute.VirtualMachine
		GraphicTypes []compute.GraphicType
		User         *User
		Request      *http.Request
	}{"Update VirtualMachine", vm, GraphicTypes, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/update", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineUpdateFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	params := compute.VirtualMachineUpdateParams{}

	vcpus, err := strconv.ParseInt(req.Form.Get("Vcpus"), 10, 16)
	if err != nil {
		http.Error(rw, "invalid vcpus value: "+err.Error(), http.StatusBadRequest)
		return
	}
	vcpusInt := int(vcpus)
	params.Vcpus = &vcpusInt

	memoryValue, err := strconv.ParseUint(req.Form.Get("MemoryValue"), 10, 32)
	if err != nil {
		http.Error(rw, "invalid memoryMb value: "+err.Error(), http.StatusBadRequest)
		return
	}
	memoryUnit := compute.NewSizeUnit(req.Form.Get("MemoryUnit"))
	if memoryUnit == compute.SizeUnitUnknown {
		http.Error(rw, "unknown memory unit: "+req.Form.Get("MemoryUnit"), http.StatusBadRequest)
		return
	}
	memory := compute.NewSize(memoryValue, memoryUnit)
	params.Memory = &memory

	autostart := req.Form.Get("Autostart") == "true"
	params.Autostart = &autostart

	guestagent := req.Form.Get("GuestAgent") == "true"
	params.GuestAgent = &guestagent

	graphicType := compute.NewGraphicType(req.Form.Get("GraphicType"))
	if graphicType == compute.GraphicTypeUnknown {
		http.Error(rw, "unknown graphic type: "+req.Form.Get("GraphicType"), http.StatusBadRequest)
		return
	}
	params.GraphicType = &graphicType

	graphicListen := req.Form.Get("GraphicListen")
	params.GraphicListen = &graphicListen

	if err := env.compute.VirtualMachineUpdate(urlvars["id"], urlvars["node"], params); err != nil {
		env.error(rw, req, err, "cannot update virtual machine", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"], "node", urlvars["node"])
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineAttachDiskFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	deviceType := compute.NewDeviceType(req.Form.Get("DeviceType"))
	if deviceType == compute.DeviceTypeUnknown {
		http.Error(rw, "unknown device type", http.StatusBadRequest)
		return
	}
	deviceBus := compute.NewDeviceBus(req.Form.Get("DeviceBus"))
	if deviceBus == compute.DeviceBusUnknown {
		http.Error(rw, "unknown device bus", http.StatusBadRequest)
		return
	}
	params := compute.VolumeAttachmentParams{
		MachineId:  urlvars["id"],
		NodeId:     urlvars["node"],
		DeviceName: req.Form.Get("DeviceName"),
		VolumePath: req.Form.Get("VolumePath"),
		DeviceType: deviceType,
		DeviceBus:  deviceBus,
	}
	if err := env.compute.VirtualMachineAttachVolume(params); err != nil {
		env.error(rw, req, err, "cannot attach disk", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"], "node", urlvars["node"])
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VirtualMachineDetachVolumeFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := env.compute.VirtualMachineDetachVolume(urlvars["id"], urlvars["node"], req.Form.Get("Path")); err != nil {
		env.error(rw, req, err, "cannot detach disk", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"], "node", urlvars["node"])
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
	networkName := req.Form.Get("Network")

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

	attachedIface := &compute.VirtualMachineAttachedInterface{
		NetworkName: networkName,
		Mac:         mac,
		Model:       "virtio",
		AccessVlan:  accessVlan,
	}
	if err := env.compute.VirtualMachineAttachInterface(id, urlvars["node"], attachedIface); err != nil {
		env.error(rw, req, err, "cannot attach interface", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", id, "node", urlvars["node"])
	http.Redirect(rw, req, redirectUrl.Path+"?tab=interfaces", http.StatusFound)
}

func (env *Environ) VirtualMachineDetachInterfaceFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	if err := env.compute.VirtualMachineDetachInterface(urlvars["id"], urlvars["node"], req.Form.Get("Mac")); err != nil {
		env.error(rw, req, err, "cannot detach interface", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"], "node", urlvars["node"])
	http.Redirect(rw, req, redirectUrl.Path+"?tab=interfaces", http.StatusFound)
}

func (env *Environ) VirtualMachineConsoleShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"], urlvars["node"])
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

	console, err := env.compute.VirtualMachineGetConsoleStream(urlvars["id"], urlvars["node"])
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
			if err := wsconn.WriteMessage(websocket.BinaryMessage, buf[0:n]); err != nil {
				env.logger.Debug().Err(err).Msg("wsconn write error")
				return
			}
		}
	}()
	for {
		_, reader, err := wsconn.NextReader()
		if err != nil {
			env.logger.Debug().Err(err).Msg("ws message error")
			return
		}
		if _, err := io.Copy(console, reader); err != nil {
			env.logger.Debug().Err(err).Msg("console write error")
			return
		}
	}
}

func (env *Environ) VirtualMachineVncShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.compute.VirtualMachineDetail(urlvars["id"], urlvars["node"])
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
	if err := env.render.HTML(rw, http.StatusOK, "virtual-machine/vnc", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineVncWs(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	wsconn, err := env.ws.Upgrade(rw, req, nil)
	if err != nil {
		env.logger.Debug().Err(err).Msg("cannot upgrade websocket connection")
		return
	}
	graphic, err := env.compute.VirtualMachineGetGraphicStream(urlvars["id"], urlvars["node"])
	if err != nil {
		http.Error(rw, "Cannot get vm graphic: "+err.Error(), http.StatusServiceUnavailable)
		env.logger.Warn().Err(err).Msg("failed to establish tcp connection")
		return
	}

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := graphic.Read(buf)
			if err != nil {
				env.logger.Debug().Err(err).Msg("graphic read error")
				return
			}
			if err := wsconn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				env.logger.Debug().Err(err).Msg("wsconn write error")
				return
			}
		}
	}()
	for {
		t, buf, err := wsconn.ReadMessage()
		if err != nil {
			env.logger.Debug().Err(err).Msg("ws message error")
			return
		}
		switch t {
		case websocket.BinaryMessage:
			if _, err := graphic.Write(buf); err != nil {
				env.logger.Debug().Err(err).Msg("graphic write error")
				return
			}
		case websocket.PingMessage:
			if err := wsconn.WriteMessage(websocket.PongMessage, buf); err != nil {
				log.Println(err)
				return
			}
		default:
		}

	}
}
