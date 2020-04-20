package web

import (
	"net/http"
	"strconv"
	"subuk/vmango/compute"

	"github.com/gorilla/mux"
)

func (env *Environ) NodeDetail(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	options := compute.NodeGetOptions{}
	filterCpuNumaIdRaw := req.URL.Query().Get("cpu_numa")
	if filterCpuNumaIdRaw != "" {
		options.CpuNumaIdFilter = true
		filterNumaId, err := strconv.ParseInt(filterCpuNumaIdRaw, 10, 16)
		if err != nil {
			http.Error(rw, "invalid numa id filer", http.StatusBadRequest)
			return
		}
		options.CpuNumaId = int(filterNumaId)
	}

	node, err := env.nodes.Get(urlvars["id"], options)
	if err != nil {
		env.error(rw, req, err, "node get failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Node    *compute.Node
		User    *User
		Request *http.Request
	}{"Node " + node.Id, node, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "node/detail", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) NodeList(rw http.ResponseWriter, req *http.Request) {
	nodes, err := env.nodes.List(compute.NodeListOptions{})
	if err != nil {
		env.error(rw, req, err, "node list failed", http.StatusInternalServerError)
		return
	}
	volumePools, err := env.volpools.List(compute.VolumePoolListOptions{})
	if err != nil {
		env.error(rw, req, err, "cannot fetch volume pools", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title       string
		Nodes       []*compute.Node
		VolumePools []*compute.VolumePool
		User        *User
	}{"Node Info", nodes, volumePools, env.Session(req).AuthUser()}
	if err := env.render.HTML(rw, http.StatusOK, "node/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}
