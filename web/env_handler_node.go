package web

import (
	"net/http"
	"subuk/vmango/compute"
)

func (env *Environ) NodeList(rw http.ResponseWriter, req *http.Request) {
	nodes, err := env.compute.NodeList()
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
