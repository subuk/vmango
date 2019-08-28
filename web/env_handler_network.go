package web

import (
	"net/http"
	"subuk/vmango/compute"
)

func (env *Environ) NetworkList(rw http.ResponseWriter, req *http.Request) {
	networks, err := env.compute.NetworkList()
	if err != nil {
		env.error(rw, req, err, "network list failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title    string
		Networks []*compute.Network
		User     *User
		Request  *http.Request
	}{"Networks", networks, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "network/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}
