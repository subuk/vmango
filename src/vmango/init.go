package vmango

import (
	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/unrolled/render"
	"net/http"
	"vmango/models"
)

type Context struct {
	Render  *render.Render
	Storage models.Storage
	Images  models.Imagerep
	Logger  *logrus.Logger
	Meta    *bolt.DB
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

	vars := map[string]interface{}{"Request": r, "Error": err.Error()}

	switch err.(type) {
	default:
		h.ctx.Logger.WithField("error", err).Warn("failed to handle request")
		h.ctx.Render.HTML(w, http.StatusInternalServerError, "500", vars)
	case *ErrNotFound:
		h.ctx.Render.HTML(w, http.StatusNotFound, "404", vars)
	case *ErrForbidden:
		h.ctx.Render.HTML(w, http.StatusForbidden, "403", vars)
	case *ErrBadRequest:
		h.ctx.Render.HTML(w, http.StatusBadRequest, "400", vars)
	}
}
