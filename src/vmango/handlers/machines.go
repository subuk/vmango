package handlers

import (
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"net/http"
	"vmango"
	"vmango/models"
)

func MachineDelete(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machine := &models.VirtualMachine{
		Name: mux.Vars(req)["name"],
	}
	if req.Method == "POST" {
		if err := ctx.IPPool.Fetch(machine); err != nil {
			return err
		}
		if err := ctx.Machines.Remove(machine); err != nil {
			return err
		}
		if machine.Ip != nil {
			if err := ctx.IPPool.Release(machine.Ip); err != nil {
				return err
			}
		}
		url, err := ctx.Router.Get("machine-list").URL()
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
		return nil
	} else {
		if exists, err := ctx.Machines.Get(machine); err != nil {
			return fmt.Errorf("failed to fetch machine info: %s", err)
		} else if !exists {
			return vmango.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/delete", map[string]interface{}{
			"Request": req,
			"Machine": machine,
		})
	}
	return nil
}

func MachineStateChange(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machine := &models.VirtualMachine{
		Name: mux.Vars(req)["name"],
	}
	if exists, err := ctx.Machines.Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine info: %s", err)
	} else if !exists {
		return vmango.NotFound(fmt.Sprintf("Machine with name %s not found", machine.Name))
	}

	action := mux.Vars(req)["action"]
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
		default:
			return vmango.BadRequest(fmt.Sprintf("unknown action '%s' requested", action))
		}
		url, err := ctx.Router.Get("machine-detail").URL("name", machine.Name)
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
		})
		return nil
	}
}

func MachineList(ctx *vmango.Context, w http.ResponseWriter, req *http.Request) error {
	machines := &models.VirtualMachineList{}
	if err := ctx.Machines.List(machines); err != nil {
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
	if err := ctx.IPPool.Fetch(machine); err != nil {
		return err
	}

	ctx.Render.HTML(w, http.StatusOK, "machines/detail", map[string]interface{}{
		"Request": req,
		"Machine": machine,
	})
	return nil
}

type MachineAddFormData struct {
	Name   string
	Plan   string
	Image  string
	SSHKey []string
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

		image := &models.Image{FullName: form.Image}
		if exists, err := ctx.Images.Get(image); err != nil {
			return err
		} else if !exists {
			return vmango.BadRequest(fmt.Sprintf(`image "%s" not found`, form.Image))
		}
		sshkeys := []*models.SSHKey{}
		for _, keyName := range form.SSHKey {
			key := models.SSHKey{Name: keyName}
			if exists, err := ctx.SSHKeys.Get(&key); err != nil {
				return fmt.Errorf("failed to fetch ssh key %s: %s", keyName, err)
			} else if !exists {
				return vmango.BadRequest(fmt.Sprintf("ssh key '%s' doesn't exist", keyName))
			}
			sshkeys = append(sshkeys, &key)
		}

		ip := &models.IP{}
		if exists, err := ctx.IPPool.Get(ip); err != nil {
			return fmt.Errorf("cannot find ip address for vm: %s", err)
		} else if !exists {
			return fmt.Errorf("no free ip address availaible")
		}

		vm := &models.VirtualMachine{
			Name:      form.Name,
			Memory:    plan.Memory,
			Cpus:      plan.Cpus,
			ImageName: image.FullName,
			Ip:        ip,
			SSHKeys:   sshkeys,
		}

		if exists, err := ctx.Machines.Get(vm); err != nil {
			return err
		} else if exists {
			return vmango.BadRequest(fmt.Sprintf("machine with name '%s' already exists", vm.Name))
		}
		if err := ctx.Machines.Create(vm, image, plan); err != nil {
			return fmt.Errorf("failed to create machine: %s", err)
		}
		vm = &models.VirtualMachine{Name: vm.Name}
		if exists, err := ctx.Machines.Get(vm); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("failed to fetch info for just created machine: %s")
		}

		if err := ctx.IPPool.Assign(ip, vm); err != nil {
			return fmt.Errorf("failed to mark ip as used: %s", err)
		}
		ip.UsedBy = vm.Name
		if err := ctx.Machines.Start(vm); err != nil {
			return fmt.Errorf("failed to start machine: %s", err)
		}
		url, err := ctx.Router.Get("machine-list").URL()
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
	} else {
		plans := []*models.Plan{}
		if err := ctx.Plans.List(&plans); err != nil {
			return fmt.Errorf("failed to fetch plan list: %s", err)
		}
		ips := &models.IPList{}
		if err := ctx.IPPool.List(ips); err != nil {
			return fmt.Errorf("failed to fetch ip list: %s", err)
		}
		images := []*models.Image{}
		if err := ctx.Images.List(&images); err != nil {
			return fmt.Errorf("failed to fetch images list: %s", err)
		}
		sshkeys := []*models.SSHKey{}
		if err := ctx.SSHKeys.List(&sshkeys); err != nil {
			return fmt.Errorf("failed to fetch ssh keys list: %s", err)
		}
		ctx.Render.HTML(w, http.StatusOK, "machines/add", map[string]interface{}{
			"Request": req,
			"Plans":   plans,
			"Ips":     ips,
			"Images":  images,
			"SSHKeys": sshkeys,
		})
	}
	return nil
}
