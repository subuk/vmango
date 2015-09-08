package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func MachinesListHandler(w http.ResponseWriter, request *http.Request) {
	context := struct {
		Machines []*models.VirtualMachine
	}{models.Store.ListMachines()}
	vmango.Render.HTML(w, http.StatusOK, "machines/list", context)
}
