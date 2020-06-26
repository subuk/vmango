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
	options := compute.VirtualMachineListOptions{}
	selectedNodeIds := req.URL.Query()["node"]
	if len(selectedNodeIds) > 0 {
		options.NodeIds = selectedNodeIds
	}
	vms, err := env.vms.List(options)
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
	vm, err := env.vms.Get(urlvars["id"], urlvars["node"])
	if err != nil {
		env.error(rw, req, err, "vm get failed", http.StatusInternalServerError)
		return
	}
	volumes, err := env.volumes.List(compute.VolumeListOptions{NodeIds: []string{vm.NodeId}})
	if err != nil {
		env.error(rw, req, err, "cannot list volumes", http.StatusInternalServerError)
		return
	}
	networks, err := env.networks.List(compute.NetworkListOptions{NodeIds: []string{vm.NodeId}})
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
		if volume.AttachedTo == "" && volume.Image == "" {
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
	vm, err := env.vms.Get(urlvars["id"], urlvars["node"])
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
	if err := env.vms.Action(urlvars["id"], urlvars["node"], urlvars["action"]); err != nil {
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
		Title            string
		User             *User
		Request          *http.Request
		InterfaceModels  []string
		GraphicTypes     []compute.GraphicType
		DeviceTypes      []compute.DeviceType
		DeviceBuses      []compute.DeviceBus
		VolumeFormats    []compute.VolumeFormat
		VideoModels      []compute.VideoModel
		NodeId           string
		Nodes            []*compute.Node
		AvailableVolumes []*compute.Volume
		Images           []*compute.ImageManifest
		Pools            []*compute.VolumePool
		Networks         []*compute.Network
		Keys             []*compute.Key
		Arches           []compute.Arch
	}{
		Title:           "Create Virtual Machine",
		Request:         req,
		User:            env.Session(req).AuthUser(),
		Arches:          []compute.Arch{compute.ArchAmd64},
		DeviceTypes:     DeviceTypes,
		DeviceBuses:     DeviceBuses,
		InterfaceModels: InterfaceModels,
		GraphicTypes:    GraphicTypes,
		VolumeFormats:   UIVolumeFormats,
		VideoModels:     VideoModels,
	}

	nodes, err := env.nodes.List(compute.NodeListOptions{NoPins: true})
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

	volumes, err := env.volumes.List(compute.VolumeListOptions{NodeIds: []string{selectedNode.Id}})
	if err != nil {
		env.error(rw, req, err, "cannot list volumes", http.StatusInternalServerError)
		return
	}
	for _, volume := range volumes {
		if !volume.Available() {
			continue
		}
		data.AvailableVolumes = append(data.AvailableVolumes, volume)
	}

	images, err := env.images.List(compute.ImageManifestListOptions{})
	if err != nil {
		env.error(rw, req, err, "cannot list images", http.StatusInternalServerError)
	}
	data.Images = images

	pools, err := env.volpools.List(compute.VolumePoolListOptions{NodeIds: []string{selectedNode.Id}})
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

	networks, err := env.networks.List(compute.NetworkListOptions{NodeIds: []string{selectedNode.Id}})
	if err != nil {
		env.error(rw, req, err, "cannot list networks", http.StatusInternalServerError)
		return
	}
	data.Networks = networks

	templateName := "virtual-machine/add"
	if req.URL.Query().Get("mode") == "advanced" {
		templateName = "virtual-machine/add-advanced"
	}
	if err := env.render.HTML(rw, http.StatusOK, templateName, data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VirtualMachineAddFormProcess(rw http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	vm := &compute.VirtualMachine{
		Id:     req.Form.Get("Name"),
		NodeId: req.Form.Get("NodeId"),
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
	graphicType := compute.NewGraphicType(req.Form.Get("GraphicType"))
	if graphicType == compute.GraphicTypeUnknown {
		http.Error(rw, "unknown graphic type: "+req.Form.Get("GraphicType"), http.StatusBadRequest)
		return
	}
	videoModel := compute.NewVideoModel(req.Form.Get("VideoModel"))
	if videoModel == compute.VideoModelUnknown {
		http.Error(rw, "unknown video model: "+req.Form.Get("VideoModel"), http.StatusBadRequest)
		return
	}

	newVols := []compute.VirtualMachineManagerCreatedVolumeParams{}
	newVolsCount := len(req.Form["CreateVolumeName"])
	for idx := 0; idx < newVolsCount; idx++ {
		sizeV, err := strconv.ParseUint(req.Form["CreateVolumeSizeValue"][idx], 10, 64)
		if err != nil {
			msg := "invalid new volume " + req.Form["CreateVolumeName"][idx] + "size value"
			http.Error(rw, msg, http.StatusBadRequest)
			return
		}
		volume := compute.VirtualMachineManagerCreatedVolumeParams{
			Name:       req.Form["CreateVolumeName"][idx],
			Pool:       req.Form["CreateVolumePool"][idx],
			Format:     compute.NewVolumeFormat(req.Form["CreateVolumeFormat"][idx]),
			Size:       compute.NewSize(sizeV, compute.NewSizeUnit(req.Form["CreateVolumeSizeUnit"][idx])),
			DeviceType: compute.NewDeviceType(req.Form["CreateVolumeDeviceType"][idx]),
			DeviceBus:  compute.NewDeviceBus(req.Form["CreateVolumeDeviceBus"][idx]),
		}
		newVols = append(newVols, volume)
	}
	cloneVols := []compute.VirtualMachineManagerClonedVolumeParams{}
	cloneVolsCount := len(req.Form["CloneVolumeOriginalPath"])
	for idx := 0; idx < cloneVolsCount; idx++ {
		var sizeValue uint64
		if req.Form["CloneVolumeNewSizeValue"][idx] != "" {
			size, err := strconv.ParseUint(req.Form["CloneVolumeNewSizeValue"][idx], 10, 64)
			if err != nil {
				msg := "invalid size specified for cloned volume: " + req.Form["CloneVolumeNewSizeValue"][idx]
				http.Error(rw, msg, http.StatusBadRequest)
				return
			}
			sizeValue = size
		}
		newName := req.Form["CloneVolumeNewName"][idx]
		if newName == "__magic_root_suffix__" {
			newName = fmt.Sprintf("%s_root", vm.Id)
		}
		volume := compute.VirtualMachineManagerClonedVolumeParams{
			OriginalPath: req.Form["CloneVolumeOriginalPath"][idx],
			NewName:      newName,
			NewPool:      req.Form["CloneVolumeNewPool"][idx],
			NewFormat:    compute.NewVolumeFormat(req.Form["CloneVolumeNewFormat"][idx]),
			NewSize:      compute.NewSize(sizeValue, compute.NewSizeUnit(req.Form["CloneVolumeNewSizeUnit"][idx])),
			DeviceType:   compute.NewDeviceType(req.Form["CloneVolumeDeviceType"][idx]),
			DeviceBus:    compute.NewDeviceBus(req.Form["CloneVolumeDeviceBus"][idx]),
		}
		cloneVols = append(cloneVols, volume)
	}
	attachedVols := len(req.Form["AttachVolumePath"])
	for idx := 0; idx < attachedVols; idx++ {
		vm.Volumes = append(vm.Volumes, &compute.VirtualMachineAttachedVolume{
			Path:       req.Form["AttachVolumePath"][idx],
			DeviceType: compute.NewDeviceType(req.Form["AttachVolumeDeviceType"][idx]),
			DeviceBus:  compute.NewDeviceBus(req.Form["AttachVolumeDeviceBus"][idx]),
		})
	}

	for idx := 0; idx < len(req.Form["InterfaceNetwork"]); idx++ {
		var accessVlan uint
		accessVlanRaw := req.Form["InterfaceAccessVlan"][idx]
		if accessVlanRaw != "" {
			parsed, err := strconv.ParseUint(accessVlanRaw, 10, 16)
			if err != nil {
				http.Error(rw, "invalid vlan: "+err.Error(), http.StatusBadRequest)
				return
			}
			accessVlan = uint(parsed)
		}
		vm.Interfaces = append(vm.Interfaces, &compute.VirtualMachineAttachedInterface{
			NetworkName: req.Form["InterfaceNetwork"][idx],
			Mac:         req.Form["InterfaceMac"][idx],
			Model:       req.Form["InterfaceModel"][idx],
			AccessVlan:  accessVlan,
		})
	}

	vm.VCpus = int(vcpus)
	vm.Memory = compute.NewSize(memoryValue, memoryUnit)
	vm.GuestAgent = req.Form.Get("GuestAgent") == "true"
	vm.Hugepages = req.Form.Get("Hugepages") == "true"
	vm.Graphic = compute.VirtualMachineGraphic{
		Type: graphicType,
	}
	vm.VideoModel = videoModel
	vm.Config = &compute.VirtualMachineConfig{
		Hostname: req.Form.Get("Name"),
		Userdata: []byte(req.Form.Get("Userdata")),
	}
	for _, fp := range req.Form["Keys"] {
		key, err := env.keys.Get(fp)
		if err != nil {
			http.Error(rw, "cannot fetch key: "+err.Error(), http.StatusInternalServerError)
			return
		}
		vm.Config.Keys = append(vm.Config.Keys, key)
	}
	start := req.Form.Get("Start") == "true"
	vm.Autostart = start

	if err := env.vmanager.Create(vm, nil, cloneVols, newVols, start); err != nil {
		env.logger.Debug().Interface("vm", vm).Interface("cloneVols", cloneVols).Interface("newVols", newVols).Msg("vm create data")
		env.error(rw, req, err, "cannot create vm", http.StatusInternalServerError)
		return
	}

	redirectPath := ""
	if start {
		redirectPath = env.url("virtual-machine-console-show", "id", vm.Id, "node", vm.NodeId).Path
	} else {
		redirectPath = env.url("virtual-machine-detail", "id", vm.Id, "node", vm.NodeId).Path
	}
	http.Redirect(rw, req, redirectPath, http.StatusFound)
}

func (env *Environ) VirtualMachineDeleteFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.vms.Get(urlvars["id"], urlvars["node"])
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
	if err := env.vmanager.Delete(urlvars["id"], urlvars["node"], deleteVolumes); err != nil {
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

var VideoModels = []compute.VideoModel{
	compute.VideoModelNone,
	compute.VideoModelCirrus,
	compute.VideoModelQxl,
}

func (env *Environ) VirtualMachineUpdateFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.vms.Get(urlvars["id"], urlvars["node"])
	if err != nil {
		env.error(rw, req, err, "virtual-machine detail failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title        string
		Vm           *compute.VirtualMachine
		GraphicTypes []compute.GraphicType
		VideoModels  []compute.VideoModel
		User         *User
		Request      *http.Request
	}{"Update VirtualMachine", vm, GraphicTypes, VideoModels, env.Session(req).AuthUser(), req}
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
	vm := &compute.VirtualMachine{
		Id:         urlvars["id"],
		NodeId:     urlvars["node"],
		Autostart:  req.Form.Get("Autostart") == "true",
		GuestAgent: req.Form.Get("GuestAgent") == "true",
		VideoModel: compute.NewVideoModel(req.Form.Get("VideoModel")),
		Hugepages:  req.Form.Get("Hugepages") == "true",
		Graphic: compute.VirtualMachineGraphic{
			Type:   compute.NewGraphicType(req.Form.Get("GraphicType")),
			Listen: req.Form.Get("GraphicListen"),
		},
	}

	vcpus, err := strconv.ParseInt(req.Form.Get("Vcpus"), 10, 16)
	if err != nil {
		http.Error(rw, "invalid vcpus value: "+err.Error(), http.StatusBadRequest)
		return
	}
	vm.VCpus = int(vcpus)

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
	vm.Memory = compute.NewSize(memoryValue, memoryUnit)

	if err := env.vms.Save(vm); err != nil {
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
	attachedVolume := &compute.VirtualMachineAttachedVolume{
		Path:       req.Form.Get("VolumePath"),
		Alias:      req.Form.Get("Alias"),
		DeviceType: deviceType,
		DeviceBus:  deviceBus,
	}
	if err := env.vms.AttachVolume(urlvars["id"], urlvars["node"], attachedVolume); err != nil {
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
	if err := env.vms.DetachVolume(urlvars["id"], urlvars["node"], req.Form.Get("Path")); err != nil {
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
	if err := env.vms.AttachInterface(id, urlvars["node"], attachedIface); err != nil {
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
	if err := env.vms.DetachInterface(urlvars["id"], urlvars["node"], req.Form.Get("Mac")); err != nil {
		env.error(rw, req, err, "cannot detach interface", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("virtual-machine-detail", "id", urlvars["id"], "node", urlvars["node"])
	http.Redirect(rw, req, redirectUrl.Path+"?tab=interfaces", http.StatusFound)
}

func (env *Environ) VirtualMachineConsoleShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	vm, err := env.vms.Get(urlvars["id"], urlvars["node"])
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

	console, err := env.vms.GetConsoleStream(urlvars["id"], urlvars["node"])
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
	vm, err := env.vms.Get(urlvars["id"], urlvars["node"])
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
	graphic, err := env.vms.GetGraphicStream(urlvars["id"], urlvars["node"])
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
