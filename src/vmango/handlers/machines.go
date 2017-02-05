package handlers

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func MachineDelete(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	machine := &models.VirtualMachine{
		Name:       urlvars["name"],
		Hypervisor: urlvars["hypervisor"],
	}
	if exists, err := ctx.Machines.Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine info: %s", err)
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
	}

	if req.Method == "POST" {
		if err := ctx.Machines.Remove(machine); err != nil {
			return err
		}
		url, err := ctx.Router.Get("machine-list").URL()
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
		return nil
	} else {
		ctx.Render.HTML(w, http.StatusOK, "machines/delete", map[string]interface{}{
			"Request": req,
			"Machine": machine,
			"Title":   fmt.Sprintf("Remove machine %s", machine.Name),
		})
	}
	return nil
}

func MachineStateChange(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	machine := &models.VirtualMachine{
		Name:       urlvars["name"],
		Hypervisor: urlvars["hypervisor"],
	}
	if exists, err := ctx.Machines.Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine info: %s", err)
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
	}

	action := urlvars["action"]
	if req.Method == "POST" {
		switch action {
		case "stop":
			if err := ctx.Machines.Stop(machine); err != nil {
				return fmt.Errorf("failed to stop machine: %s", err)
			}
		case "start":
			if err := ctx.Machines.Start(machine); err != nil {
				return fmt.Errorf("failed to start machine: %s", err)
			}
		case "reboot":
			if err := ctx.Machines.Reboot(machine); err != nil {
				return fmt.Errorf("failed to reboot machine: %s", err)
			}
		default:
			return web.BadRequest(fmt.Sprintf("unknown action '%s' requested", action))
		}
		url, err := ctx.Router.Get("machine-detail").URL("name", machine.Name, "hypervisor", machine.Hypervisor)
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
		return nil
	} else {
		ctx.Render.HTML(w, http.StatusOK, "machines/changestate", map[string]interface{}{
			"Request": req,
			"Machine": machine,
			"Action":  action,
			"Title":   fmt.Sprintf("Do %s on machine %s", action, machine.Name),
		})
		return nil
	}
}

func MachineList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	machines := &models.VirtualMachineList{}
	if err := ctx.Machines.List(machines); err != nil {
		return err
	}
	ctx.RenderResponse(w, req, http.StatusOK, "machines/list", map[string]interface{}{
		"Machines": machines,
		"Title":    "Machines",
	})
	return nil
}

func MachineDetail(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	machine := &models.VirtualMachine{
		Name:       urlvars["name"],
		Hypervisor: urlvars["hypervisor"],
	}
	if exists, err := ctx.Machines.Get(machine); err != nil {
		return err
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
	}
	ctx.RenderResponse(w, req, http.StatusOK, "machines/detail", map[string]interface{}{
		"Machine": machine,
		"Title":   fmt.Sprintf("Machine %s", machine.Name),
	})
	return nil
}

type machineAddFormData struct {
	Name       string
	Plan       string
	Image      string
	Hypervisor string
	SSHKey     []string
}

func MachineAddForm(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	if req.Method == "POST" {
		if err := req.ParseForm(); err != nil {
			return err
		}
		form := &machineAddFormData{}
		if err := schema.NewDecoder().Decode(form, req.PostForm); err != nil {
			return web.BadRequest(err.Error())
		}
		plan := &models.Plan{Name: form.Plan}
		if exists, err := ctx.Plans.Get(plan); err != nil {
			return err
		} else if !exists {
			return web.BadRequest(fmt.Sprintf(`plan "%s" not found`, form.Plan))
		}

		image := &models.Image{FullName: form.Image, Hypervisor: form.Hypervisor}
		if exists, err := ctx.Images.Get(image); err != nil {
			return err
		} else if !exists {
			return web.BadRequest(fmt.Sprintf(`image "%s" not found on hypervisor "%s"`, image.FullName, image.Hypervisor))
		}
		sshkeys := []*models.SSHKey{}
		for _, keyName := range form.SSHKey {
			key := models.SSHKey{Name: keyName}
			if exists, err := ctx.SSHKeys.Get(&key); err != nil {
				return fmt.Errorf("failed to fetch ssh key %s: %s", keyName, err)
			} else if !exists {
				return web.BadRequest(fmt.Sprintf("ssh key '%s' doesn't exist", keyName))
			}
			sshkeys = append(sshkeys, &key)
		}

		vm := &models.VirtualMachine{
			Name:       form.Name,
			Memory:     plan.Memory,
			Cpus:       plan.Cpus,
			ImageName:  image.FullName,
			SSHKeys:    sshkeys,
			Hypervisor: image.Hypervisor,
		}

		if exists, err := ctx.Machines.Get(vm); err != nil {
			return err
		} else if exists {
			return web.BadRequest(fmt.Sprintf("machine with name '%s' already exists", vm.Name))
		}
		if err := ctx.Machines.Create(vm, image, plan); err != nil {
			return fmt.Errorf("failed to create machine: %s", err)
		}
		if err := ctx.Machines.Start(vm); err != nil {
			return fmt.Errorf("failed to start machine: %s", err)
		}
		url, err := ctx.Router.Get("machine-detail").URL("name", vm.Name, "hypervisor", vm.Hypervisor)
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
	} else {
		plans := []*models.Plan{}
		if err := ctx.Plans.List(&plans); err != nil {
			return fmt.Errorf("failed to fetch plan list: %s", err)
		}
		images := &models.ImageList{}
		if err := ctx.Images.List(images); err != nil {
			return fmt.Errorf("failed to fetch images list: %s", err)
		}
		sshkeys := []*models.SSHKey{}
		if err := ctx.SSHKeys.List(&sshkeys); err != nil {
			return fmt.Errorf("failed to fetch ssh keys list: %s", err)
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/add", map[string]interface{}{
			"Request": req,
			"Plans":   plans,
			"Images":  images,
			"SSHKeys": sshkeys,
			"Title":   "Create machine",
		})
	}
	return nil
}
