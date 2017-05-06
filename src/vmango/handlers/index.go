package handlers

import (
	"fmt"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func Index(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	statuses := models.StatusInfoList{}
	for _, provider := range ctx.Providers {
		status := &models.StatusInfo{}
		if err := provider.Status(status); err != nil {
			return fmt.Errorf("failed to query provider %s for status: %s", provider.Name(), err)
		}
		statuses = append(statuses, status)
	}
	ctx.RenderResponse(w, req, http.StatusOK, "index", map[string]interface{}{
		"Statuses": statuses,
		"Title":    "Server info",
	})
	return nil
}
