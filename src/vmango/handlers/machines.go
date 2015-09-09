package handlers

import (
	"fmt"
	"github.com/gorilla/mux"
	"net/http"
	"vmango"
	"vmango/models"
)

func MachineList(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machines := []*models.VirtualMachine{}
	if err := ctx.Machines.List(&machines); err != nil {
		return err
	}
	ctx.Render.HTML(w, http.StatusOK, "machines/list", map[string]interface{}{
		"Request":  req,
		"Machines": machines,
	})
	return nil
}

func MachineDetail(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machine := &models.VirtualMachine{
		Name: mux.Vars(req)["name"],
	}
	if exists, err := ctx.Machines.Get(machine); err != nil {
		return err
	} else if !exists {
		return vmango.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
	}

	ctx.Render.HTML(w, http.StatusOK, "machines/detail", map[string]interface{}{
		"Request": req,
		"Machine": machine,
	})
	return nil
}
