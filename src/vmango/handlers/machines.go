package handlers

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"net/http"
	"vmango"
	"vmango/models"
)

func MachineList(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machines := []*models.VirtualMachine{}
	if err, _ := ctx.Machines.List(&machines); err != nil {
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

type MachineAddFormData struct {
	Name  string
	Plan  string
	Image string
}

func MachineAddForm(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	if req.Method == "POST" {
		if err := req.ParseForm(); err != nil {
			return err
		}
		form := &MachineAddFormData{}
		if err := schema.NewDecoder().Decode(form, req.PostForm); err != nil {
			return vmango.BadRequest(err.Error())
		}

		plan := &models.Plan{Name: form.Plan}
		if exists, err := ctx.Plans.Get(plan); err != nil {
			return err
		} else if !exists {
			return vmango.BadRequest(fmt.Sprintf(`plan "%s" not found`, form.Plan))
		}

		image := &models.Image{Filename: form.Image}
		if exists, err := ctx.Images.Get(image); err != nil {
			return err
		} else if !exists {
			return vmango.BadRequest(fmt.Sprintf(`image "%s" not found`, form.Image))
		}

		vm := &models.VirtualMachine{
			Name:      form.Name,
			Memory:    plan.Memory,
			Cpus:      plan.Cpus,
			ImageName: image.Filename,
		}

		if err := ctx.Machines.Create(vm, image, plan); err != nil {
			return fmt.Errorf("failed to create machine: %s", err)
		}

		url, err := ctx.Router.Get("machine-list").URL()
		if err != nil {
			panic(err)
		}
		if err := ctx.Machines.Start(vm); err != nil {
			return fmt.Errorf("failed to start machine: %s", err)
		}
		return vmango.Redirect(url.Path)
	} else {
		plans := []*models.Plan{}
		if err := ctx.Plans.List(&plans); err != nil {
			return fmt.Errorf("failed to fetch plan list: %s", err)
		}
		ips := []*models.IP{}
		if err := ctx.IPPool.List(&ips); err != nil {
			return fmt.Errorf("failed to fetch ip list: %s", err)
		}
		images := []*models.Image{}
		if err := ctx.Images.List(&images); err != nil {
			return fmt.Errorf("failed to fetch images list: %s", err)
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/add", map[string]interface{}{
			"Request": req,
			"Plans":   plans,
			"Ips":     ips,
			"Images":  images,
		})
	}
	return nil
}
