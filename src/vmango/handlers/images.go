package handlers

import (
	"fmt"
	"net/http"
	"vmango"
	"vmango/models"
)

func ImageList(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	images := []*models.Image{}
	if err := ctx.Images.List(&images); err != nil {
		return fmt.Errorf("failed to fetch images list: %s", err)
	}
	ctx.Render.HTML(w, http.StatusOK, "images/list", map[string]interface{}{
		"Request": req,
		"Images":  images,
	})
	return nil
}
