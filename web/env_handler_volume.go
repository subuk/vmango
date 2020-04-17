package web

import (
	"net/http"
	"strconv"
	"strings"
	"subuk/vmango/compute"

	"github.com/gorilla/mux"
)

var UIVolumeFormats = []compute.VolumeFormat{
	compute.VolumeFormatQcow2,
	compute.VolumeFormatRaw,
}

func (env *Environ) VolumeList(rw http.ResponseWriter, req *http.Request) {
	selectedNodeId := req.URL.Query().Get("node")
	selectedPool := req.URL.Query().Get("pool")
	nodes, err := env.nodes.List()

	var filterPoolNames []string
	if selectedPool != "" {
		filterPoolNames = append(filterPoolNames, selectedPool)
	}

	var filterNodeIds []string
	if selectedNodeId != "" {
		filterNodeIds = append(filterNodeIds, selectedNodeId)
	}
	if err != nil {
		env.error(rw, req, err, "nodes list failed", http.StatusInternalServerError)
		return
	}
	volumes, err := env.volumes.List(compute.VolumeListOptions{NodeIds: filterNodeIds, PoolNames: filterPoolNames})
	if err != nil {
		env.error(rw, req, err, "volume list failed", http.StatusInternalServerError)
		return
	}
	var pools []*compute.VolumePool
	nodePools, err := env.volpools.List(compute.VolumePoolListOptions{NodeIds: filterNodeIds})
	if err != nil {
		env.error(rw, req, err, "pool list failed", http.StatusInternalServerError)
		return
	}
	pools = nodePools
	data := struct {
		Title         string
		NodeId        string
		Pool          string
		Volumes       []*compute.Volume
		Nodes         []*compute.Node
		Pools         []*compute.VolumePool
		VolumeFormats []compute.VolumeFormat
		User          *User
		Request       *http.Request
	}{"Volumes", selectedNodeId, selectedPool, volumes, nodes, pools, UIVolumeFormats, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "volume/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VolumeDeleteFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	volume, err := env.volumes.Get(path, urlvars["node"])
	if err != nil {
		env.error(rw, req, err, "volume get failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Volume  *compute.Volume
		User    *User
		Request *http.Request
	}{"Delete Volume", volume, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "volume/delete", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VolumeCloneFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	volume, err := env.volumes.Get(path, urlvars["node"])
	if err != nil {
		env.error(rw, req, err, "volume get failed", http.StatusInternalServerError)
		return
	}
	pools, err := env.volpools.List(compute.VolumePoolListOptions{NodeIds: []string{volume.NodeId}})
	if err != nil {
		env.error(rw, req, err, "pool list failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title         string
		Volume        *compute.Volume
		Pools         []*compute.VolumePool
		VolumeFormats []compute.VolumeFormat
		User          *User
		Request       *http.Request
	}{"Clone Volume", volume, pools, UIVolumeFormats, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "volume/clone", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VolumeCloneFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	sizeValue, err := strconv.ParseUint(req.Form.Get("SizeValue"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid new volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	sizeUnit := compute.NewSizeUnit(req.Form.Get("SizeUnit"))
	if sizeUnit == compute.SizeUnitUnknown {
		http.Error(rw, "unknown size unit: "+req.Form.Get("SizeUnit"), http.StatusBadRequest)
		return
	}
	params := compute.VolumeCloneParams{
		Format:       compute.NewVolumeFormat(req.Form.Get("Format")),
		NodeId:       urlvars["node"],
		OriginalPath: path,
		NewName:      req.Form.Get("Name"),
		NewPool:      req.Form.Get("Pool"),
		NewSize:      compute.NewSize(sizeValue, sizeUnit),
	}
	if _, err := env.volumes.Clone(params); err != nil {
		env.error(rw, req, err, "volume clone failed", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("volume-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VolumeResizeFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	volume, err := env.volumes.Get(path, urlvars["node"])
	if err != nil {
		env.error(rw, req, err, "volume get failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Volume  *compute.Volume
		User    *User
		Request *http.Request
	}{"Resize Volume", volume, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "volume/resize", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VolumeResizeFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)

	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	newSizeValue, err := strconv.ParseUint(req.Form.Get("SizeValue"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid new volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	newSizeUnit := compute.NewSizeUnit(req.Form.Get("SizeUnit"))
	if newSizeUnit == compute.SizeUnitUnknown {
		http.Error(rw, "unknown size unit: "+req.Form.Get("SizeUnit"), http.StatusBadRequest)
		return
	}
	if err := env.volumes.Resize(path, urlvars["node"], compute.NewSize(newSizeValue, newSizeUnit)); err != nil {
		env.error(rw, req, err, "volume clone failed", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("volume-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VolumeDeleteFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	if err := env.volumes.Delete(path, urlvars["node"]); err != nil {
		env.error(rw, req, err, "cannot delete volume", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("volume-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VolumeAddFormProcess(rw http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	sizeValue, err := strconv.ParseUint(req.Form.Get("SizeValue"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	sizeUnit := compute.NewSizeUnit(req.Form.Get("SizeUnit"))
	if sizeUnit == compute.SizeUnitUnknown {
		http.Error(rw, "unknown size unit: "+req.Form.Get("SizeUnit"), http.StatusBadRequest)
		return
	}
	params := compute.VolumeCreateParams{
		NodeId: req.Form.Get("NodeId"),
		Name:   req.Form.Get("Name"),
		Pool:   req.Form.Get("Pool"),
		Format: compute.NewVolumeFormat(req.Form.Get("Format")),
		Size:   compute.NewSize(sizeValue, sizeUnit),
	}
	if _, err := env.volumes.Create(params); err != nil {
		env.error(rw, req, err, "cannot add key", http.StatusInternalServerError)
		return
	}
	redirectUrl := req.URL.Query().Get("next")
	if redirectUrl == "" {
		redirectUrl = env.url("volume-list").Path
	}
	http.Redirect(rw, req, redirectUrl, http.StatusFound)
}
