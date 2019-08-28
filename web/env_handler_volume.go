package web

import (
	"net/http"
	"strconv"
	"strings"
	"subuk/vmango/compute"

	"github.com/gorilla/mux"
)

func (env *Environ) VolumeList(rw http.ResponseWriter, req *http.Request) {
	volumes, err := env.compute.VolumeList()
	if err != nil {
		env.error(rw, req, err, "volume list failed", http.StatusInternalServerError)
		return
	}
	pools, err := env.compute.VolumePoolList()
	if err != nil {
		env.error(rw, req, err, "pool list failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title         string
		Volumes       []*compute.Volume
		Pools         []*compute.VolumePool
		VolumeFormats []compute.VolumeFormat
		User          *User
		Request       *http.Request
	}{"Volumes", volumes, pools, []compute.VolumeFormat{compute.FormatQcow2, compute.FormatRaw}, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "volume/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) VolumeDeleteFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	volume, err := env.compute.VolumeGet(path)
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

func (env *Environ) VolumeDeleteFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	if err := env.compute.VolumeDelete(path); err != nil {
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
	name := req.Form.Get("Name")
	pool := req.Form.Get("Pool")
	size, err := strconv.ParseUint(req.Form.Get("Size"), 10, 64)
	var format compute.VolumeFormat
	if err != nil {
		http.Error(rw, "invalid volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch rawFormat := req.Form.Get("Format"); rawFormat {
	default:
		http.Error(rw, "invalid volume format '"+rawFormat+"'", http.StatusBadRequest)
		return
	case "qcow2":
		format = compute.FormatQcow2
	case "raw":
		format = compute.FormatRaw
	}

	if _, err := env.compute.VolumeCreate(pool, name, format, size); err != nil {
		env.error(rw, req, err, "cannot add key", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("volume-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}
