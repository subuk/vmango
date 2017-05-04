package handlers

import (
	"fmt"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func ImageList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	allImages := map[string]models.ImageList{}
	for _, provider := range ctx.Providers {
		images := models.ImageList{}
		if err := provider.Images().List(&images); err != nil {
			return fmt.Errorf("failed to fetch images list: %s", err)
		}
		allImages[provider.Name()] = images
	}
	ctx.RenderResponse(w, req, http.StatusOK, "images/list", map[string]interface{}{
		"Images": allImages,
		"Title":  "Images",
	})
	return nil
}
