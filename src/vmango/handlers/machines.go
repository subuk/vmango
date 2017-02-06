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
	hypervisor := ctx.Hypervisors.Get(urlvars["hypervisor"])
	if hypervisor == nil {
		return web.NotFound(fmt.Sprintf("hypervisor '%s' not found", urlvars["hypervisor"]))
	}
	machine := &models.VirtualMachine{
		Name: urlvars["name"],
	}
	if exists, err := hypervisor.Machines.Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine info: %s", err)
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
	}

	if req.Method == "POST" {
		if err := hypervisor.Machines.Remove(machine); err != nil {
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
	hypervisor := ctx.Hypervisors.Get(urlvars["hypervisor"])
	if hypervisor == nil {
		return web.NotFound(fmt.Sprintf("hypervisor '%s' not found", urlvars["hypervisor"]))
	}
	machine := &models.VirtualMachine{
		Name: urlvars["name"],
	}

	if exists, err := hypervisor.Machines.Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine info: %s", err)
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
	}

	action := urlvars["action"]
	if req.Method == "POST" {
		switch action {
		case "stop":
			if err := hypervisor.Machines.Stop(machine); err != nil {
				return fmt.Errorf("failed to stop machine: %s", err)
			}
		case "start":
			if err := hypervisor.Machines.Start(machine); err != nil {
				return fmt.Errorf("failed to start machine: %s", err)
			}
		case "reboot":
			if err := hypervisor.Machines.Reboot(machine); err != nil {
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
	for _, hypervisor := range ctx.Hypervisors {
		if err := hypervisor.Machines.List(machines); err != nil {
			return fmt.Errorf("failed to query hypervisor %s: %s", hypervisor.Name, err)
		}
	}
	ctx.RenderResponse(w, req, http.StatusOK, "machines/list", map[string]interface{}{
		"Machines": machines,
		"Title":    "Machines",
	})
	return nil
}

func MachineDetail(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	hypervisor := ctx.Hypervisors.Get(urlvars["hypervisor"])
	if hypervisor == nil {
		return web.NotFound(fmt.Sprintf("hypervisor '%s' not found", urlvars["hypervisor"]))
	}
	machine := &models.VirtualMachine{
		Name: urlvars["name"],
	}
	if exists, err := hypervisor.Machines.Get(machine); err != nil {
		return err
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with name %s not found on hypervisor %s", machine.Name, hypervisor.Name))
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

func (data *machineAddFormData) Validate() error {
	errors := schema.MultiError{}
	if data.Name == "" {
		errors["Name"] = fmt.Errorf("name required")
	}
	if data.Plan == "" {
		errors["Plan"] = fmt.Errorf("plan required")
	}
	if data.Image == "" {
		errors["Image"] = fmt.Errorf("image required")
	}
	if data.Hypervisor == "" {
		errors["Hypervisor"] = fmt.Errorf("hypervisor required")
	}
	if len(errors) <= 0 {
		return nil
	}
	return errors
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
		if err := form.Validate(); err != nil {
			return web.BadRequest(err.Error())
		}

		hypervisor := ctx.Hypervisors.Get(form.Hypervisor)
		if hypervisor == nil {
			return web.BadRequest(fmt.Sprintf(`hypervisor "%s" not found`, form.Hypervisor))
		}

		plan := &models.Plan{Name: form.Plan}
		if exists, err := ctx.Plans.Get(plan); err != nil {
			return err
		} else if !exists {
			return web.BadRequest(fmt.Sprintf(`plan "%s" not found`, form.Plan))
		}

		image := &models.Image{FullName: form.Image}
		if exists, err := hypervisor.Images.Get(image); err != nil {
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
			Name:      form.Name,
			Memory:    plan.Memory,
			Cpus:      plan.Cpus,
			ImageName: image.FullName,
			SSHKeys:   sshkeys,
		}

		if exists, err := hypervisor.Machines.Get(vm); err != nil {
			return err
		} else if exists {
			return web.BadRequest(fmt.Sprintf("machine with name '%s' already exists on hypervisor '%s'", vm.Name, hypervisor.Name))
		}
		if err := hypervisor.Machines.Create(vm, image, plan); err != nil {
			return fmt.Errorf("failed to create machine: %s", err)
		}
		if err := hypervisor.Machines.Start(vm); err != nil {
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
		images := map[string]*models.ImageList{}
		for _, hypervisor := range ctx.Hypervisors {
			hvImages := &models.ImageList{}
			if err := hypervisor.Images.List(hvImages); err != nil {
				return fmt.Errorf("failed to fetch images list from hypervisor %s: %s", hypervisor.Name, err)
			}
			images[hypervisor.Name] = hvImages
		}
		sshkeys := []*models.SSHKey{}
		if err := ctx.SSHKeys.List(&sshkeys); err != nil {
			return fmt.Errorf("failed to fetch ssh keys list: %s", err)
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/add", map[string]interface{}{
			"Request":     req,
			"Plans":       plans,
			"Images":      images,
			"Hypervisors": ctx.Hypervisors,
			"SSHKeys":     sshkeys,
			"Title":       "Create machine",
		})
	}
	return nil
}
