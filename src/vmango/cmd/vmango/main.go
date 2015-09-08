package main

import (
	"flag"
	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	"github.com/meatballhat/negroni-logrus"
	"github.com/unrolled/render"
	"net/http"
	"vmango"
	"vmango/handlers"
	"vmango/models"
)

var (
	LISTEN_ADDR   = flag.String("listen", "0.0.0.0:8000", "Listen address")
	TEMPLATE_PATH = flag.String("template-path", "templates", "Template path")
	STATIC_PATH   = flag.String("static-path", "static", "Static path")
)

func main() {
	flag.Parse()

	renderer := render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: true,
		Directory:     *TEMPLATE_PATH,
	})

	storage, err := models.NewLibvirtStorage("qemu:///system")
	if err != nil {
		panic(err)
	}

	ctx := &vmango.Context{
		Render:  renderer,
		Storage: storage,
	}

	router := mux.NewRouter()

	router.Handle("/", vmango.NewHandler(ctx, handlers.IndexHandler))
	router.Handle("/machines", vmango.NewHandler(ctx, handlers.MachinesListHandler))
	router.HandleFunc("/static{name:.*}", handlers.MakeStaticHandler(*STATIC_PATH))

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	log.WithField("address", *LISTEN_ADDR).Info("starting server")
	log.Fatal(http.ListenAndServe(*LISTEN_ADDR, n))
}
