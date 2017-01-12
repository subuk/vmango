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
	text_template "text/template"
	"time"
	"vmango"
	"vmango/dal"
	"vmango/handlers"
)

var (
	LISTEN_ADDR   = flag.String("listen", "0.0.0.0:8000", "Listen address")
	LIBVIRT_URL   = flag.String("libvirt-url", "qemu:///system", "Libvirt connection url")
	META_ADDR     = flag.String("meta-listen", "192.168.122.1:8001", "Metadata server addr")
	TEMPLATE_PATH = flag.String("template-path", "templates", "Template path")
	STATIC_PATH   = flag.String("static-path", "static", "Static path")
	METADB_PATH   = flag.String("metadb-path", "vmango.db", "Metadata database path")
	IMAGES_PATH   = flag.String("images-path", "images", "Machine images repository path")
	VM_TEMPLATE   = flag.String("vm-template", "vm.xml.in", "Virtual machine configuration template")
)

func main() {
	flag.Parse()
	log.SetLevel(log.InfoLevel)

	router := mux.NewRouter().StrictSlash(true)

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
	vmtpl, err := text_template.ParseFiles(*VM_TEMPLATE)
	if err != nil {
		log.WithError(err).WithField("filename", *VM_TEMPLATE).Fatal("failed to parse machine template")
	}
	machines, err := dal.NewLibvirtMachinerep(*LIBVIRT_URL, vmtpl)
	if err != nil {
		log.WithError(err).Fatal("failed to initialize libvirt-kvm machines")
	}

	imagerep := dal.NewLocalfsImagerep(*IMAGES_PATH)

	metadb, err := bolt.Open(*METADB_PATH, 0600, nil)
	if err != nil {
		log.WithError(err).Fatal("failed to open metadata db")
	}

	planrep := dal.NewBoltPlanrep(metadb)
	ippool := dal.NewBoltIPPool(metadb)

	ctx := &vmango.Context{
		Render:   renderer,
		Router:   router,
		Machines: machines,
		Logger:   log.New(),
		Images:   imagerep,
		IPPool:   ippool,
		Plans:    planrep,
	}

	router.Handle("/", vmango.NewHandler(ctx, handlers.Index)).Name("index")
	router.Handle("/machines/", vmango.NewHandler(ctx, handlers.MachineList)).Name("machine-list")
	router.Handle("/machines/add/", vmango.NewHandler(ctx, handlers.MachineAddForm)).Name("machine-add")
	router.Handle("/machines/{name:[^/]+}/", vmango.NewHandler(ctx, handlers.MachineDetail)).Name("machine-detail")
	router.Handle("/images/", vmango.NewHandler(ctx, handlers.ImageList)).Name("image-list")
	router.Handle("/ipaddress/", vmango.NewHandler(ctx, handlers.IPList)).Name("ip-list")
	router.Handle("/plans/", vmango.NewHandler(ctx, handlers.PlanList)).Name("plan-list")

	router.HandleFunc("/static{name:.*}", handlers.MakeStaticHandler(*STATIC_PATH)).Name("static")

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	log.WithField("address", *LISTEN_ADDR).Info("starting server")
	log.Fatal(http.ListenAndServe(*LISTEN_ADDR, n))
}
