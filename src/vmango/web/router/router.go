package router

import (
	"net/http"
	"vmango/handlers"
	"vmango/web"

	"github.com/gorilla/mux"
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

	machineAddProcess := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineAddFormProcess, web.LimitMethods("POST"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/add/", machineAddProcess).Methods("POST")

	machineAddShow := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineAddFormShow, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/add/", machineAddShow).Name("machine-add")

	machineDetail := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDetail, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{provider:[^/]+}/{id:[^/]+}/", machineDetail).Name("machine-detail")

	machineStateChangeProcess := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineStateChangeFormProcess, web.LimitMethods("POST"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{provider:[^/]+}/{id:[^/]+}/{action:(?:start|stop|reboot)}/", machineStateChangeProcess).Methods("POST")

	machineStateChangeShow := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineStateChangeFormShow, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{provider:[^/]+}/{id:[^/]+}/{action:(?:start|stop|reboot)}/", machineStateChangeShow).Name("machine-changestate")

	machineDeleteProcess := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDeleteFormProcess, web.LimitMethods("POST"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{provider:[^/]+}/{id:[^/]+}/delete/", machineDeleteProcess).Methods("POST")

	machineDeleteShow := csrfProtect(web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDeleteFormShow, web.LimitMethods("GET", "HEAD"),
		web.SessionAuthenticationRequired,
	)))
	router.Handle("/machines/{provider:[^/]+}/{id:[^/]+}/delete/", machineDeleteShow).Name("machine-delete")

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
	machineAdd := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineAddFormProcess, web.LimitMethods("POST"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/machines/", machineAdd).
		Methods("POST").
		Name("api-machine-add")

	machineList := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineList, web.LimitMethods("GET", "HEAD"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/machines/", machineList).
		Name("api-machine-list")

	machineDeleteUrl := "/machines/{provider:[^/]+}/{id:[^/]+}/"
	machineDelete := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDeleteFormProcess, web.LimitMethods("DELETE"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle(machineDeleteUrl, machineDelete).Methods("DELETE").Name("api-machine-delete")

	machineDetail := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineDetail, web.LimitMethods("GET", "HEAD"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/machines/{provider:[^/]+}/{id:[^/]+}/", machineDetail).
		Name("api-machine-detail")

	machineStateChangeUrl := "/machines/{provider:[^/]+}/{id:[^/]+}/{action:(?:start|stop|reboot)}/"
	machineStateChange := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.MachineStateChangeFormProcess, web.LimitMethods("POST"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle(machineStateChangeUrl, machineStateChange).Name("api-machine-changestate")

	imageList := web.NewHandler(ctx, web.ApplyDecorators(
		handlers.ImageList, web.LimitMethods("GET", "HEAD"),
		web.APIAuthenticationRequired, web.ForceJsonResponse,
	))
	router.Handle("/images/", imageList).Name("api-image-list")

	return router
}
