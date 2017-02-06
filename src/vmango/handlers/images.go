package handlers

import (
	"fmt"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func ImageList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	images := models.ImageList{}
	for _, hypervisor := range ctx.Hypervisors {
		if err := hypervisor.Images.List(&images); err != nil {
			return fmt.Errorf("failed to fetch images list: %s", err)
		}
	}
	ctx.RenderResponse(w, req, http.StatusOK, "images/list", map[string]interface{}{
		"Images": images,
		"Title":  "Images",
	})
	return nil
}
