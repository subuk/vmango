package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func IndexHandler(ctx *vmango.Context, w http.ResponseWriter, request *http.Request) (int, error) {
	ctx.Render.HTML(w, http.StatusOK, "index", struct {
		Request  *http.Request
		Machines []*models.VirtualMachine
	}{request, ctx.Storage.ListMachines()})
	return 200, nil
}
