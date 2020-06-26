package web

import (
	"net/http"
	"subuk/vmango/compute"
)

func (env *Environ) ImageList(rw http.ResponseWriter, req *http.Request) {
	images, err := env.images.List(compute.ImageManifestListOptions{})
	if err != nil {
		env.error(rw, req, err, "image list failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Images  []*compute.ImageManifest
		User    *User
		Request *http.Request
	}{"Images", images, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "image/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}
