package web

import (
	"crypto/sha1"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"subuk/vmango/config"
	"time"

	"github.com/gorilla/mux"
)

func (env *Environ) Static(cfg *config.Config) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		name := "static/" + mux.Vars(req)["name"]
		content, err := Asset(name)
		if err != nil {
			if err.Error() == fmt.Sprintf("Asset %s not found", name) {
				log.Println(err)
				http.NotFound(rw, req)
				return

			}
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		info, err := AssetInfo(name)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		contentType := mime.TypeByExtension(filepath.Ext(name))
		rw.Header().Set("Content-Type", contentType)
		rw.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		rw.Header().Set("Last-Modified", info.ModTime().UTC().Format(time.RFC1123))
		rw.Header().Set("ETag", fmt.Sprintf("%x", sha1.Sum(content)))
		if !cfg.Web.Debug {
			cacheTimeoutSec := 12 * 60 * 60
			rw.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", cacheTimeoutSec))
			rw.Header().Set("Expires", time.Now().Add(time.Duration(cacheTimeoutSec)*time.Second).UTC().Format(time.RFC1123))
		}

		if _, err := rw.Write(content); err != nil {
			log.Printf("failed to write response: %s", err)
			return
		}
	}
}
