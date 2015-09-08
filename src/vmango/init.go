package vmango

import (
	"github.com/unrolled/render"
	"net/http"
	"vmango/models"
)

type Context struct {
	Render  *render.Render
	Storage *models.LibvirtStorage
}

type HandlerFunc func(*Context, http.ResponseWriter, *http.Request) (int, error)

type Handler struct {
	ctx    *Context
	handle HandlerFunc
}

func NewHandler(ctx *Context, handle HandlerFunc) *Handler {
	return &Handler{ctx, handle}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, err := h.handle(h.ctx, w, r)
	if err != nil {
		switch status {
		case http.StatusNotFound:
			h.ctx.Render.HTML(w, status, "404", nil)
		case http.StatusForbidden:
			h.ctx.Render.HTML(w, status, "403", nil)
		default:
			http.Error(w, http.StatusText(status), status)
		}
	}
}
