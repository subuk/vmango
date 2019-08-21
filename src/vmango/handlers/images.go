package handlers

import (
	"fmt"
	"net/http"
	"vmango/web"
)

func ImageList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	images, err := ctx.Machines.ListImages()
	if err != nil {
		return fmt.Errorf("failed to list images: %s", err)
	}

	ctx.RenderResponse(w, req, http.StatusOK, "images/list", map[string]interface{}{
		"Images": images,
		"Title":  "Images",
	})
	return nil
}
