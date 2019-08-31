package web

import (
	"net/http"
	"subuk/vmango/compute"
)

func (env *Environ) HostInfo(rw http.ResponseWriter, req *http.Request) {
	hostinfo, err := env.compute.HostInfo()
	if err != nil {
		env.error(rw, req, err, "hostinfo failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title    string
		HostInfo *compute.HostInfo
		User     *User
	}{"Host Info", hostinfo, env.Session(req).AuthUser()}
	if err := env.render.HTML(rw, http.StatusOK, "hostinfo", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}
