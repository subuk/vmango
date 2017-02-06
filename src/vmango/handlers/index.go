package handlers

import (
	"fmt"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func Index(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	machines := &models.VirtualMachineList{}
	servers := &models.ServerList{}
	for _, hypervisor := range ctx.Hypervisors {
		if err := hypervisor.Machines.List(machines); err != nil {
			return fmt.Errorf("failed to query hypervisor %s: %s", hypervisor.Name, err)
		}
		if err := hypervisor.Machines.ServerInfo(servers); err != nil {
			return fmt.Errorf("failed to query hypervisor %s: %s", hypervisor.Name, err)
		}
	}

	ctx.Render.HTML(w, http.StatusOK, "index", map[string]interface{}{
		"Request":  req,
		"Machines": machines,
		"Servers":  servers,
		"Title":    "Server info",
	})
	return nil
}
