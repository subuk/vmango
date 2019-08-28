package web

import (
	"net/http"
	"strconv"
	"strings"
	"subuk/vmango/compute"

	"github.com/gorilla/mux"
)

func parseVolumeFormat(raw string) compute.VolumeFormat {
	switch raw {
	default:
		return compute.FormatUnknown
	case "qcow2":
		return compute.FormatQcow2
	case "raw":
		return compute.FormatRaw
	}
}

var UIVolumeFormats = []compute.VolumeFormat{compute.FormatQcow2, compute.FormatRaw}

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
	}{"Volumes", volumes, pools, UIVolumeFormats, env.Session(req).AuthUser(), req}
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

func (env *Environ) VolumeCloneFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	path := strings.Replace(urlvars["path"], "%2F", "/", -1)
	volume, err := env.compute.VolumeGet(path)
	if err != nil {
		env.error(rw, req, err, "volume get failed", http.StatusInternalServerError)
		return
	}
	pools, err := env.compute.VolumePoolList()
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
	name := req.Form.Get("Name")
	pool := req.Form.Get("Pool")
	size, err := strconv.ParseUint(req.Form.Get("Size"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid new volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	format := parseVolumeFormat(req.Form.Get("Format"))
	if format == compute.FormatUnknown {
		http.Error(rw, "invalid volume format '"+req.Form.Get("Format")+"'", http.StatusBadRequest)
		return
	}
	if _, err := env.compute.VolumeClone(path, name, pool, format, size); err != nil {
		env.error(rw, req, err, "volume clone failed", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("volume-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) VolumeResizeFormShow(rw http.ResponseWriter, req *http.Request) {
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
	newSize, err := strconv.ParseUint(req.Form.Get("Size"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid new volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := env.compute.VolumeResize(path, newSize); err != nil {
		env.error(rw, req, err, "volume clone failed", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("volume-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
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
	if err != nil {
		http.Error(rw, "invalid volume size: "+err.Error(), http.StatusBadRequest)
		return
	}
	format := parseVolumeFormat(req.Form.Get("Format"))
	if format == compute.FormatUnknown {
		http.Error(rw, "invalid volume format '"+req.Form.Get("Format")+"'", http.StatusBadRequest)
		return
	}

	if _, err := env.compute.VolumeCreate(pool, name, format, size); err != nil {
		env.error(rw, req, err, "cannot add key", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("volume-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}
