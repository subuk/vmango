package web

import (
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/unrolled/render"
	"net/http"
	"time"
	"vmango/dal"
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

	Plans    dal.Planrep
	Machines dal.Machinerep
	Images   dal.Imagerep
	IPPool   dal.IPPool
	SSHKeys  dal.SSHKeyrep
	AuthDB   dal.Authrep
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
	if r.URL.Path != "/login/" && r.URL.Path != "/login" {
		session := h.ctx.Session(r)
		if !session.IsAuthenticated() {
			http.Redirect(w, r, "/login/?next="+r.URL.String(), http.StatusFound)
			return
		}
	}
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
