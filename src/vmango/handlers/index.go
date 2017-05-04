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
	for _, provider := range ctx.Providers {
		if err := provider.Machines().List(machines); err != nil {
			return fmt.Errorf("failed to query provider %s: %s", provider, err)
		}
		if err := provider.Machines().ServerInfo(servers); err != nil {
			return fmt.Errorf("failed to query provider %s: %s", provider, err)
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
