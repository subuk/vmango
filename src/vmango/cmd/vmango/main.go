package main

import (
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	"github.com/meatballhat/negroni-logrus"
	"github.com/unrolled/render"
	"html/template"
	"net/http"
	"strings"
	"time"
	"vmango"
	"vmango/handlers"
	"vmango/models"
)

var (
	LISTEN_ADDR   = flag.String("listen", "0.0.0.0:8000", "Listen address")
	TEMPLATE_PATH = flag.String("template-path", "templates", "Template path")
	STATIC_PATH   = flag.String("static-path", "static", "Static path")
	METADB_PATH   = flag.String("metadb-path", "vmango-meta.db", "Metadata database path")
	IMAGES_PATH   = flag.String("images-path", "images", "Machine images repository path")
)

func main() {
	flag.Parse()

	router := mux.NewRouter()

	renderer := render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: true,
		Directory:     *TEMPLATE_PATH,
		Funcs: []template.FuncMap{
			template.FuncMap{
				"HasPrefix": strings.HasPrefix,
				"HumanizeDate": func(date time.Time) string {
					return date.Format("Mon Jan 2 15:04:05 -0700 MST 2006")
				},
				"Url": func(name string, params ...string) (string, error) {
					route := router.Get(name)
					if route == nil {
						return "", fmt.Errorf("route named %s not found", name)
					}
					url, err := route.URL(params...)
					if err != nil {
						return "", err
					}
					return url.Path, nil
				},
			},
		},
	})

	storage, err := models.NewLibvirtStorage("qemu:///system")
	if err != nil {
		log.WithError(err).Fatal("failed to initialize libvirt-kvm storage")
	}

	imagerep := models.NewLocalfsImagerep(*IMAGES_PATH)

	metadb, err := bolt.Open(*METADB_PATH, 0600, nil)
	if err != nil {
		log.WithError(err).Fatal("failed to open metadata db")
	}

	ctx := &vmango.Context{
		Render:  renderer,
		Storage: storage,
		Logger:  log.New(),
		Meta:    metadb,
		Images:  imagerep,
	}

	router.Handle("/", vmango.NewHandler(ctx, handlers.Index)).Name("index")
	router.Handle("/machines", vmango.NewHandler(ctx, handlers.MachineList)).Name("machine-list")
	router.Handle("/machines/{name:.+}", vmango.NewHandler(ctx, handlers.MachineDetail)).Name("machine-detail")
	router.Handle("/images", vmango.NewHandler(ctx, handlers.ImageList)).Name("image-list")

	router.HandleFunc("/static{name:.*}", handlers.MakeStaticHandler(*STATIC_PATH)).Name("static")

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	log.WithField("address", *LISTEN_ADDR).Info("starting server")
	log.Fatal(http.ListenAndServe(*LISTEN_ADDR, n))
}
