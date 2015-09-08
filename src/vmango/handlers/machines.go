package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func MachinesListHandler(w http.ResponseWriter, request *http.Request) {
	vmango.Render.HTML(w, http.StatusOK, "machines/list", struct {
		Request  *http.Request
		Machines []*models.VirtualMachine
	}{
		request,
		models.Store.ListMachines(),
	})
}
