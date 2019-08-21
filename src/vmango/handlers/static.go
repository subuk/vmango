package handlers

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"time"
	"vmango/web"

	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
)

func ServeAsset(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	name := req.URL.Path[1:]
	content, err := web.Asset(name)
	if err != nil {
		if err.Error() == fmt.Sprintf("Asset %s not found", name) {
			return web.NotFound(err.Error())
		}
		return err
	}
	info, err := web.AssetInfo(name)
	if err != nil {
		return err
	}
	contentType := mime.TypeByExtension(filepath.Ext(name))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int(ctx.StaticCache.Seconds())))
	w.Header().Set("Expires", time.Now().Add(ctx.StaticCache).UTC().Format(time.RFC1123))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(time.RFC1123))
	ctx.Render.Data(w, http.StatusOK, content)
	return nil
}

func MakeStaticHandler(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		name := mux.Vars(request)["name"]
		logrus.WithField("name", name).Debug("serving static file")
		http.ServeFile(w, request, filepath.Join(root, name))
	}
}
