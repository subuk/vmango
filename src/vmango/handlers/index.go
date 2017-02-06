package handlers

import (
	"net/http"
	"vmango/models"
	"vmango/web"
)

func Index(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	machines := &models.VirtualMachineList{}
	if err := ctx.Machines.List(machines); err != nil {
		return err
	}
	servers := []*models.Server{}
	if err := ctx.Machines.ServerInfo(&servers); err != nil {
		return err
	}
	ctx.Render.HTML(w, http.StatusOK, "index", map[string]interface{}{
		"Request":  req,
		"Machines": machines,
		"Servers":  servers,
		"Title":    "Server info",
	})
	return nil
}
