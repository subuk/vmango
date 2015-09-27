package cloudmeta

import (
	"github.com/Sirupsen/logrus"
	"net/http"
	"vmango"
)

type Context struct {
	Logger   *logrus.Logger
	Resolver *LibvirtResolver
}

type HandlerFunc func(*Context, http.ResponseWriter, *http.Request) error

type Handler struct {
	ctx    *Context
	handle HandlerFunc
}

func NewHandler(ctx *Context, handle HandlerFunc) *Handler {
	return &Handler{ctx, handle}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h.handle(h.ctx, w, r)
	if err == nil {
		return
	}

	switch err.(type) {
	default:
		h.ctx.Logger.WithField("error", err).Warn("failed to handle request")
		http.Error(w, err.Error(), http.StatusInternalServerError)
	case *vmango.ErrNotFound:
		http.NotFound(w, r)
	case *vmango.ErrForbidden:
		http.Error(w, err.Error(), http.StatusForbidden)
	case *vmango.ErrBadRequest:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
