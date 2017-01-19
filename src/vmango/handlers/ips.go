package handlers

import (
	"fmt"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func IPList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	ips := &models.IPList{}
	if err := ctx.IPPool.List(ips); err != nil {
		return fmt.Errorf("failed to fetch ip list: %s", err)
	}
	ctx.Render.HTML(w, http.StatusOK, "ips/list", map[string]interface{}{
		"Request": req,
		"Ips":     ips,
	})
	return nil
}
