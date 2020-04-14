package web

import (
	"net/http"
	"subuk/vmango/compute"

	"github.com/gorilla/mux"
)

func (env *Environ) KeyList(rw http.ResponseWriter, req *http.Request) {
	keys, err := env.keys.List()
	if err != nil {
		env.error(rw, req, err, "key list failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Keys    []*compute.Key
		User    *User
		Request *http.Request
	}{"Keys", keys, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "key/list", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) KeyShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	key, err := env.keys.Get(urlvars["fingerprint"])
	if err != nil {
		env.error(rw, req, err, "key get failed", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(http.StatusOK)
	rw.Write(key.Value)
}

func (env *Environ) KeyDeleteFormShow(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	key, err := env.keys.Get(urlvars["fingerprint"])
	if err != nil {
		env.error(rw, req, err, "key get failed", http.StatusInternalServerError)
		return
	}
	data := struct {
		Title   string
		Key     *compute.Key
		User    *User
		Request *http.Request
	}{"Delete Key", key, env.Session(req).AuthUser(), req}
	if err := env.render.HTML(rw, http.StatusOK, "key/delete", data); err != nil {
		env.error(rw, req, err, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (env *Environ) KeyDeleteFormProcess(rw http.ResponseWriter, req *http.Request) {
	urlvars := mux.Vars(req)
	if err := env.keys.Delete(urlvars["fingerprint"]); err != nil {
		env.error(rw, req, err, "cannot delete key", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("key-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}

func (env *Environ) KeyAddFormProcess(rw http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	key := req.Form.Get("Key")
	if key == "" {
		http.Error(rw, "no key content specified", http.StatusBadRequest)
		return
	}
	if err := env.keys.Add(key); err != nil {
		env.error(rw, req, err, "cannot add key", http.StatusInternalServerError)
		return
	}
	redirectUrl := env.url("key-list")
	http.Redirect(rw, req, redirectUrl.Path, http.StatusFound)
}
