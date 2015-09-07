package handlers

import (
	"net/http"
	"vmango"
	"vmango/models"
)

func IndexHandler(w http.ResponseWriter, request *http.Request) {
	data := map[string]interface{}{}
	data["vms"] = models.VirtualMachineList()
	vmango.Render.HTML(w, http.StatusOK, "index", data)
}
