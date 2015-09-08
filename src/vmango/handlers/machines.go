package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func MachinesListHandler(ctx *vmango.Context, w http.ResponseWriter, request *http.Request) (int, error) {
	ctx.Render.HTML(w, http.StatusOK, "machines/list", struct {
		Request  *http.Request
		Machines []*models.VirtualMachine
	}{
		request,
		ctx.Storage.ListMachines(),
	})
	return 200, nil
}
