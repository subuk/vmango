package web

import (
	"net/http"
	"subuk/vmango/compute"
)

func (env *Environ) VolumeList(rw http.ResponseWriter, req *http.Request) {
	volumes, err := env.compute.VolumeList()
	if err != nil {
		env.error(rw, req, err, "volume list failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Volumes []*compute.Volume
		User    *User
	}{"Volumes", volumes, env.Session(req).AuthUser()}
	if err := env.render.HTML(rw, http.StatusOK, "volume/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}
