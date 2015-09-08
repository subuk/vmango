package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func IndexHandler(w http.ResponseWriter, request *http.Request) {
	vmango.Render.HTML(w, http.StatusOK, "index", struct {
		Request  *http.Request
		Machines []*models.VirtualMachine
	}{request, models.Store.ListMachines()})
}
