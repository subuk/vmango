package handlers

import (
	"fmt"
	"net/http"
	"vmango/models"
	"vmango/web"
)

func SSHKeyList(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	keys := []*models.SSHKey{}
	if err := ctx.SSHKeys.List(&keys); err != nil {
		return fmt.Errorf("failed to fetch ssh keys list: %s", err)
	}
	ctx.RenderResponse(w, req, http.StatusOK, "sshkeys/list", map[string]interface{}{
		"SSHKeys": keys,
	})
	return nil
}
