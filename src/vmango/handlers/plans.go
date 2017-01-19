package handlers

import (
	"fmt"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func PlanList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	plans := []*models.Plan{}
	if err := ctx.Plans.List(&plans); err != nil {
		return fmt.Errorf("failed to fetch plan list: %s", err)
	}
	ctx.Render.HTML(w, http.StatusOK, "plans/list", map[string]interface{}{
		"Request": req,
		"Plans":   plans,
	})
	return nil
}
