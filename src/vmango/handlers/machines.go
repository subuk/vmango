package handlers

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"net/http"
	"strings"
	"vmango/models"
	"vmango/web"
)

func MachineDelete(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	provider := ctx.Providers.Get(urlvars["provider"])
	if provider == nil {
		return web.NotFound(fmt.Sprintf("provider '%s' not found", urlvars["provider"]))
	}
	machine := &models.VirtualMachine{
		Id: urlvars["id"],
	}
	if exists, err := provider.Machines().Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine info: %s", err)
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with id %s not found", machine.Id))
	}

	if req.Method == "POST" || req.Method == "DELETE" {
		if err := provider.Machines().Remove(machine); err != nil {
			return err
		}
		ctx.RenderDeleted(w, req, map[string]interface{}{
			"Message": fmt.Sprintf("Machine %s deleted", machine.Name),
		}, "machine-list")
		return nil
	} else {
		ctx.Render.HTML(w, http.StatusOK, "machines/delete", map[string]interface{}{
			"Request":  req,
			"Provider": provider.Name(),
			"Machine":  machine,
			"Title":    fmt.Sprintf("Remove machine %s", machine.Name),
		})
	}
	return nil
}

func MachineStateChange(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	provider := ctx.Providers.Get(urlvars["provider"])
	if provider == nil {
		return web.NotFound(fmt.Sprintf("provider '%s' not found", urlvars["provider"]))
	}
	machine := &models.VirtualMachine{
		Id: urlvars["id"],
	}

	if exists, err := provider.Machines().Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine info: %s", err)
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with id %s not found", machine.Id))
	}

	action := urlvars["action"]
	if req.Method == "POST" || req.Method == "PUT" {
		switch action {
		case "stop":
			if err := provider.Machines().Stop(machine); err != nil {
				return fmt.Errorf("failed to stop machine: %s", err)
			}
		case "start":
			if err := provider.Machines().Start(machine); err != nil {
				return fmt.Errorf("failed to start machine: %s", err)
			}
		case "reboot":
			if err := provider.Machines().Reboot(machine); err != nil {
				return fmt.Errorf("failed to reboot machine: %s", err)
			}
		default:
			return web.BadRequest(fmt.Sprintf("unknown action '%s' requested", action))
		}
		ctx.RenderRedirect(w, req, map[string]interface{}{
			"Message": fmt.Sprintf("Action %s done for machine %s", action, machine.Name),
		}, "machine-detail", "id", machine.Id, "provider", provider.Name())
		return nil
	} else {
		ctx.Render.HTML(w, http.StatusOK, "machines/changestate", map[string]interface{}{
			"Request":  req,
			"Machine":  machine,
			"Action":   action,
			"Provider": provider.Name(),
			"Title":    fmt.Sprintf("Do %s on machine %s", action, machine.Name),
		})
		return nil
	}
}

func MachineList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	allMachines := map[string]models.VirtualMachineList{}
	for _, provider := range ctx.Providers {
		machines := models.VirtualMachineList{}
		if err := provider.Machines().List(&machines); err != nil {
			return fmt.Errorf("failed to query provider %s: %s", provider, err)
		}
		allMachines[provider.Name()] = machines
	}
	ctx.RenderResponse(w, req, http.StatusOK, "machines/list", map[string]interface{}{
		"Machines": allMachines,
		"Title":    "Machines",
	})
	return nil
}

func MachineDetail(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	provider := ctx.Providers.Get(urlvars["provider"])
	if provider == nil {
		return web.NotFound(fmt.Sprintf("provider '%s' not found", urlvars["provider"]))
	}
	machine := &models.VirtualMachine{
		Id: urlvars["id"],
	}
	if exists, err := provider.Machines().Get(machine); err != nil {
		return err
	} else if !exists {
		return web.NotFound(fmt.Sprintf("Machine with id %s not found on provider %s", machine.Id, provider))
	}
	ctx.RenderResponse(w, req, http.StatusOK, "machines/detail", map[string]interface{}{
		"Machine":  machine,
		"Provider": provider.Name(),
		"Title":    fmt.Sprintf("Machine %s", machine.Name),
	})
	return nil
}

type machineAddFormData struct {
	Name     string
	Plan     string
	Image    string
	Provider string
	Userdata string
	SSHKey   []string
	CSRF     string
}

func (data *machineAddFormData) Validate() error {
	errors := schema.MultiError{}
	data.Userdata = strings.TrimSpace(data.Userdata) + "\n"
	if data.Name == "" {
		errors["Name"] = fmt.Errorf("name required")
	}
	if data.Plan == "" {
		errors["Plan"] = fmt.Errorf("plan required")
	}
	if data.Image == "" {
		errors["Image"] = fmt.Errorf("image required")
	}
	if data.Provider == "" {
		errors["Provider"] = fmt.Errorf("provider required")
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
		provider := ctx.Providers.Get(form.Provider)
		if provider == nil {
			return web.BadRequest(fmt.Sprintf(`provider "%s" not found`, form.Provider))
		}

		plan := &models.Plan{Name: form.Plan}
		if exists, err := ctx.Plans.Get(plan); err != nil {
			return err
		} else if !exists {
			return web.BadRequest(fmt.Sprintf(`plan "%s" not found`, form.Plan))
		}

		image := &models.Image{Id: form.Image}
		if exists, err := provider.Images().Get(image); err != nil {
			return err
		} else if !exists {
			return web.BadRequest(fmt.Sprintf(`image "%s" not found on provider "%s"`, image.Id, provider))
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
			Name:     form.Name,
			SSHKeys:  sshkeys,
			Userdata: form.Userdata,
		}

		if err := provider.Machines().Create(vm, image, plan); err != nil {
			return fmt.Errorf("failed to create machine: %s", err)
		}
		if err := provider.Machines().Start(vm); err != nil {
			return fmt.Errorf("failed to start machine: %s", err)
		}
		ctx.RenderCreated(w, req, map[string]interface{}{
			"Message": fmt.Sprintf("Machine %s (%s) created", vm.Name, vm.Id),
		}, "machine-detail", "id", vm.Id, "provider", provider.Name())
	} else {
		plans := []*models.Plan{}
		if err := ctx.Plans.List(&plans); err != nil {
			return fmt.Errorf("failed to fetch plan list: %s", err)
		}
		images := map[string]*models.ImageList{}
		for _, provider := range ctx.Providers {
			hvImages := &models.ImageList{}
			if err := provider.Images().List(hvImages); err != nil {
				return fmt.Errorf("failed to fetch images list from provider %s: %s", provider.Name(), err)
			}
			images[provider.Name()] = hvImages
		}
		sshkeys := []*models.SSHKey{}
		if err := ctx.SSHKeys.List(&sshkeys); err != nil {
			return fmt.Errorf("failed to fetch ssh keys list: %s", err)
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/add", map[string]interface{}{
			"Request":   req,
			"Plans":     plans,
			"Images":    images,
			"Providers": ctx.Providers,
			"SSHKeys":   sshkeys,
			"Title":     "Create machine",
		})
	}
	return nil
}
