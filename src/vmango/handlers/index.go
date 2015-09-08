package handlers

import (
	"net/http"
	"vmango"
)

func Index(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	ctx.Render.HTML(w, http.StatusOK, "index", map[string]interface{}{
		"Request": req,
	})
	return nil
}
