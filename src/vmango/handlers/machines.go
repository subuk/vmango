package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"vmango/domain"
	"vmango/web"

	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
)

func MachineDelete(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	providerId := urlvars["provider"]
	machineId := urlvars["id"]

	if req.Method == "POST" || req.Method == "DELETE" {
		if err := ctx.Machines.RemoveMachine(providerId, machineId); err != nil {
			return err
		}
		ctx.RenderDeleted(w, req, map[string]interface{}{
			"Message": fmt.Sprintf("Machine %s of provider %s deleted", machineId, providerId),
		}, "machine-list")
		return nil
	} else {
		machine, err := ctx.Machines.GetMachine(providerId, machineId)
		if err != nil {
			return fmt.Errorf("failed to fetch machine %s of provider %s: %s", machineId, providerId, err)
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/delete", map[string]interface{}{
			"Request":  req,
			"Provider": providerId,
			"Machine":  machine,
			"Title":    fmt.Sprintf("Remove machine %s", machine.Name),
		})
	}
	return nil
}

func MachineStateChange(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	providerId := urlvars["provider"]
	machineId := urlvars["machine"]
	action := urlvars["action"]

	if req.Method == "POST" || req.Method == "PUT" {
		if err := ctx.Machines.DoAction(providerId, machineId, action); err != nil {
			return fmt.Errorf("failed to %s machine: %s", action, err)
		}
		ctx.RenderRedirect(w, req, map[string]interface{}{
			"Message": fmt.Sprintf("Action %s done for machine %s of provider %s", action, providerId, machineId),
		}, "machine-detail", "id", machineId, "provider", providerId)
		return nil
	} else {
		machine, err := ctx.Machines.GetMachine(providerId, machineId)
		if err != nil {
			return err
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/changestate", map[string]interface{}{
			"Request":  req,
			"Machine":  machine,
			"Action":   action,
			"Provider": providerId,
			"Title":    fmt.Sprintf("Do %s on machine %s", action, machine.Name),
		})
		return nil
	}
}

func MachineList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	machines, err := ctx.Machines.ListMachines()
	if err != nil {
		return fmt.Errorf("failed to list machines: %s", err)
	}
	fmt.Println(machines)
	ctx.RenderResponse(w, req, http.StatusOK, "machines/list", map[string]interface{}{
		"Machines": machines,
		"Title":    "Machines",
	})
	return nil
}

func MachineDetail(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	urlvars := mux.Vars(req)
	providerId := urlvars["provider"]
	machineId := urlvars["id"]

	machine, err := ctx.Machines.GetMachine(providerId, machineId)
	if err != nil {
		return err
	}
	ctx.RenderResponse(w, req, http.StatusOK, "machines/detail", map[string]interface{}{
		"Machine":  machine,
		"Provider": providerId,
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
		params := domain.MachineCreateParams{
			Provider: form.Provider,
			Name:     form.Name,
			Plan:     form.Plan,
			Image:    form.Image,
			SSHKeys:  form.SSHKey,
			Userdata: form.Userdata,
			Creator:  ctx.AuthUser.Name,
		}
		vm, err := ctx.Machines.CreateMachine(params)
		if err != nil {
			return fmt.Errorf("failed to create machine: %s", err)
		}
		ctx.RenderCreated(w, req, map[string]interface{}{
			"Message": fmt.Sprintf("Machine %s (%s) created", vm.Name, vm.Id),
		}, "machine-detail", "id", vm.Id, "provider", form.Provider)
	} else {
		plans, err := ctx.Machines.ListPlans()
		if err != nil {
			return fmt.Errorf("failed to fetch plan list: %s", err)
		}
		images, err := ctx.Machines.ListImages()
		if err != nil {
			return fmt.Errorf("failed to fetch images list: %s", err)
		}
		sshkeys, err := ctx.Machines.ListKeys()
		if err != nil {
			return fmt.Errorf("failed to fetch ssh keys list: %s", err)
		}
		providers, err := ctx.Machines.ListProviders()
		if err != nil {
			return fmt.Errorf("failed to fetch providers list: %s", err)
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/add", map[string]interface{}{
			"Request":   req,
			"Plans":     plans,
			"Images":    images,
			"Providers": providers,
			"SSHKeys":   sshkeys,
			"Title":     "Create machine",
		})
	}
	return nil
}
