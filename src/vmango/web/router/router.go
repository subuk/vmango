package router

import (
	"github.com/gorilla/mux"
	"vmango/handlers"
	"vmango/web"
)

func New(ctx *web.Context) *mux.Router {
	router := mux.NewRouter().StrictSlash(true)

	router.Handle("/", web.NewHandler(ctx, handlers.Index)).Name("index")
	router.Handle("/machines/", web.NewHandler(ctx, handlers.MachineList)).Name("machine-list")
	router.Handle("/machines/add/", web.NewHandler(ctx, handlers.MachineAddForm)).Name("machine-add")
	router.Handle("/machines/{name:[^/]+}/", web.NewHandler(ctx, handlers.MachineDetail)).Name("machine-detail")
	router.Handle("/machines/{name:[^/]+}/{action:(?:start|stop|reboot)}/", web.NewHandler(ctx, handlers.MachineStateChange)).Name("machine-changestate")
	router.Handle("/machines/{name:[^/]+}/delete/", web.NewHandler(ctx, handlers.MachineDelete)).Name("machine-delete")
	router.Handle("/images/", web.NewHandler(ctx, handlers.ImageList)).Name("image-list")
	router.Handle("/login/", web.NewHandler(ctx, handlers.Login)).Name("login")
	router.Handle("/logout/", web.NewHandler(ctx, handlers.Logout)).Name("logout")

	router.Handle("/static/{name:.*}", web.NewHandler(ctx, handlers.ServeAsset)).Name("static")
	return router
}
