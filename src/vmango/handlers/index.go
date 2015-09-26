package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func Index(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machines := &models.VirtualMachineList{}
	if err := ctx.Machines.List(machines); err != nil {
		return err
	}
	ctx.Render.HTML(w, http.StatusOK, "index", map[string]interface{}{
		"Request":  req,
		"Machines": machines,
	})
	return nil
}
