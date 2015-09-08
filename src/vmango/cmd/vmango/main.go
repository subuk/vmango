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

	vmango.Render = render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: true,
		Directory:     *TEMPLATE_PATH,
	})

	storage, err := models.NewLibvirtStorage("qemu:///system")
	if err != nil {
		panic(err)
	}
	models.Store = storage
	router := mux.NewRouter()

	router.HandleFunc("/", handlers.IndexHandler)
	router.HandleFunc("/static{name:.*}", handlers.MakeStaticHandler(*STATIC_PATH))

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	log.WithField("address", *LISTEN_ADDR).Info("listening")
	http.ListenAndServe(*LISTEN_ADDR, n)
}
