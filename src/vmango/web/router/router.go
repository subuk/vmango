package router

import (
	"github.com/gorilla/mux"
	"net/http"
	"vmango/handlers"
	"vmango/web"
)

type CSRFProtector func(http.Handler) http.Handler

func New(ctx *web.Context, csrfProtect CSRFProtector) *mux.Router {
	router := mux.NewRouter()
	addWebRoutes(router, ctx, csrfProtect)
	addApiRoutes(router.PathPrefix("/api/").Subrouter(), ctx)
	return router
}

func addWebRoutes(router *mux.Router, ctx *web.Context, csrfProtect CSRFProtector) *mux.Router {
	index := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.Index, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/", index).Name("index")

	machineList := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineList, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/", machineList).Name("machine-list")

	machineAdd := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineAddForm, web.LimitMethods("GET", "HAED", "POST"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/add/", machineAdd).Name("machine-add")

	machineDetail := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDetail, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{hypervisor:[^/]+}/{name:[^/]+}/", machineDetail).Name("machine-detail")

	machineStateChange := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineStateChange, web.LimitMethods("GET", "HEAD", "POST"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{hypervisor:[^/]+}/{name:[^/]+}/{action:(?:start|stop|reboot)}/", machineStateChange).Name("machine-changestate")

	machineDelete := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDelete, web.LimitMethods("GET", "HEAD", "POST"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{hypervisor:[^/]+}/{name:[^/]+}/delete/", machineDelete).Name("machine-delete")

	imageList := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.ImageList, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/images/", imageList).Name("image-list")

	login := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.Login, web.LimitMethods("GET", "HEAD", "POST"),
	)))
	router.Handle("/login/", login).Name("login")

	logout := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.Logout, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/logout/", logout).Name("logout")

	static := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.ServeAsset, web.LimitMethods("GET"),
	))
	router.Handle("/static/{name:.*}", static).Name("static")

	return router
}

func addApiRoutes(router *mux.Router, ctx *web.Context) *mux.Router {
	machineList := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineList, web.LimitMethods("GET", "HEAD"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/machines/", machineList).
		Methods("GET", "HEAD", "DELETE", "PUT").
		Name("api-machine-list")

	machineAdd := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineAddForm, web.LimitMethods("POST"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/machines/", machineAdd).
		Methods("POST").
		Name("api-machine-add")

	machineDetail := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDetail, web.LimitMethods("GET", "HEAD"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/machines/{hypervisor:[^/]+}/{name:[^/]+}/", machineDetail).
		Methods("GET", "HEAD", "PUT", "POST").
		Name("api-machine-detail")

	machineStateChangeUrl := "/machines/{hypervisor:[^/]+}/{name:[^/]+}/{action:(?:start|stop|reboot)}/"
	machineStateChange := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineStateChange, web.LimitMethods("POST"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle(machineStateChangeUrl, machineStateChange).Name("api-machine-changestate")

	machineDeleteUrl := "/machines/{hypervisor:[^/]+}/{name:[^/]+}/"
	machineDelete := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDelete, web.LimitMethods("DELETE"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle(machineDeleteUrl, machineDelete).Methods("DELETE").Name("api-machine-delete")

	imageList := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.ImageList, web.LimitMethods("GET", "HEAD"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/images/", imageList).Name("api-image-list")

	return router
}
