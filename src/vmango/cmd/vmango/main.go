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
)

var (
	LISTEN_ADDR = flag.String("listen", "0.0.0.0:8000", "Listen address")
)

func main() {
	flag.Parse()

	vmango.Render = render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: true,
	})

	vmango.DB = vmango.NewDatabase("qemu:///system")

	router := mux.NewRouter()

	router.HandleFunc("/", handlers.IndexHandler)
	router.HandleFunc("/static/{name:.*}", handlers.StaticHandler)

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	log.WithField("address", *LISTEN_ADDR).Info("listening")
	http.ListenAndServe(*LISTEN_ADDR, n)
}
