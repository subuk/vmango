package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func Index(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machines := []*models.VirtualMachine{}
	err, activeMachinesCount := ctx.Machines.List(&machines);
	if err != nil {
		return err
	}
	ctx.Render.HTML(w, http.StatusOK, "index", map[string]interface{}{
		"Request": req,
		"ActiveMachinesCount": activeMachinesCount,
	})
	return nil
}
