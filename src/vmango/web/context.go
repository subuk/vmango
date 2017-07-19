package web

import (
	"net/http"
	"time"
	"vmango/dal"
	"vmango/models"

	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/unrolled/render"
)

const SESSION_NAME = "vmango"

type SessionData struct {
	*sessions.Session
}

func (session *SessionData) AuthUser() string {
	if authuser, ok := session.Values["authuser"]; ok {
		return authuser.(string)
	}
	return ""
}

func (session *SessionData) IsAuthenticated() bool {
	return session.AuthUser() != ""
}

func (session *SessionData) SetAuthUser(username string) {
	session.Values["authuser"] = username
}

type Context struct {
	Render       *render.Render
	Router       *mux.Router
	Logger       *logrus.Logger
	SessionStore sessions.Store
	StaticCache  time.Duration
	AuthUser     *models.User

	Plans     dal.Planrep
	Providers dal.Providers
	SSHKeys   dal.SSHKeyrep
	AuthDB    dal.Authrep
}

func (ctx *Context) RenderRedirect(w http.ResponseWriter, req *http.Request, bindings map[string]interface{}, routeName string, params ...string) {
	format, ok := req.Context().Value("format").(int)
	if !ok {
		format = FORMAT_HTML
	}
	switch format {
	default:
		url, err := ctx.Router.Get(routeName).URLPath(params...)
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
	case FORMAT_JSON_API:
		ctx.Render.JSON(w, http.StatusOK, bindings)
	}
}

func (ctx *Context) RenderDeleted(w http.ResponseWriter, req *http.Request, bindings map[string]interface{}, routeName string, params ...string) {
	format, ok := req.Context().Value("format").(int)
	if !ok {
		format = FORMAT_HTML
	}
	switch format {
	default:
		url, err := ctx.Router.Get(routeName).URLPath(params...)
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
	case FORMAT_JSON_API:
		ctx.Render.JSON(w, http.StatusNoContent, bindings)
	}
}

func (ctx *Context) RenderCreated(w http.ResponseWriter, req *http.Request, bindings map[string]interface{}, routeName string, params ...string) {
	format, ok := req.Context().Value("format").(int)
	if !ok {
		format = FORMAT_HTML
	}
	switch format {
	default:
		url, err := ctx.Router.Get(routeName).URLPath(params...)
		if err != nil {
			panic(err)
		}
		http.Redirect(w, req, url.Path, http.StatusFound)
	case FORMAT_JSON_API:
		url, err := ctx.Router.Get("api-" + routeName).URLPath(params...)
		if err != nil {
			panic(err)
		}
		w.Header().Set("Location", url.Path)
		ctx.Render.JSON(w, http.StatusCreated, bindings)
	}
}

func (ctx *Context) RenderResponse(w http.ResponseWriter, req *http.Request, status int, name string, binding map[string]interface{}) {
	format, ok := req.Context().Value("format").(int)
	if !ok {
		format = FORMAT_HTML
	}
	switch format {
	case FORMAT_JSON_API:
		ctx.Render.JSON(w, status, binding)
	default:
		binding["Request"] = req
		ctx.Render.HTML(w, status, name, binding)
	}
}

func (ctx *Context) Session(r *http.Request) *SessionData {
	session, err := ctx.SessionStore.Get(r, SESSION_NAME)
	if err != nil {
		panic(err)
	}
	return &SessionData{Session: session}
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

	vars := map[string]interface{}{"Error": err.Error()}

	switch err.(type) {
	default:
		h.ctx.Logger.WithField("error", err).Warn("failed to handle request")
		h.ctx.RenderResponse(w, r, http.StatusInternalServerError, "500", vars)
	case *ErrNotFound:
		h.ctx.RenderResponse(w, r, http.StatusNotFound, "404", vars)
	case *ErrForbidden:
		h.ctx.RenderResponse(w, r, http.StatusForbidden, "403", vars)
	case *ErrBadRequest:
		h.ctx.RenderResponse(w, r, http.StatusBadRequest, "400", vars)
	case *ErrNotImplemented:
		h.ctx.RenderResponse(w, r, http.StatusNotImplemented, "501", vars)
	}
}
