package handlers

import (
	"fmt"
	"net/http"
	"vmango/web"
)

func Index(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	statuses, err := ctx.Machines.Status()
	if err != nil {
		return fmt.Errorf("failed fetch status status: %s", err)
	}
	ctx.RenderResponse(w, req, http.StatusOK, "index", map[string]interface{}{
		"Statuses": statuses,
		"Title":    "Server info",
	})
	return nil
}
