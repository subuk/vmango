package main

import (
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"
	"github.com/libvirt/libvirt-go"
	"github.com/meatballhat/negroni-logrus"
	"github.com/unrolled/render"
	"html/template"
	"net/http"
	"strings"
	text_template "text/template"
	"time"
	"vmango"
	"vmango/cfg"
	"vmango/dal"
	"vmango/handlers"
)

var (
	CONFIG_PATH = flag.String("config", "vmango.conf", "Path to configuration file")
)

func main() {
	flag.Parse()
	log.SetLevel(log.InfoLevel)

	config, err := cfg.ParseConfig(*CONFIG_PATH)
	if err != nil {
		log.WithError(err).WithField("filename", *CONFIG_PATH).Fatal("failed to parse config")
	}

	router := mux.NewRouter().StrictSlash(true)

	renderer := render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: true,
		Directory:     config.TemplatePath,
		Funcs: []template.FuncMap{
			template.FuncMap{
				"HasPrefix": strings.HasPrefix,
				"HumanizeDate": func(date time.Time) string {
					return date.Format("Mon Jan 2 15:04:05 -0700 MST 2006")
				},
				"Capitalize": strings.Title,
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

	vmtpl, err := text_template.ParseFiles(config.Hypervisor.VmTemplate)
	if err != nil {
		log.WithError(err).WithField("filename", config.Hypervisor.VmTemplate).Fatal("failed to parse machine template")
	}
	virtConn, err := libvirt.NewConnect(config.Hypervisor.Url)
	if err != nil {
		log.WithError(err).Fatal("failed to connect to libvirt")
	}

	machines, err := dal.NewLibvirtMachinerep(virtConn, vmtpl, config.Hypervisor.Network)
	if err != nil {
		log.WithError(err).Fatal("failed to initialize libvirt-kvm machines")
	}

	imagerep := dal.NewLibvirtImagerep(virtConn, config.Hypervisor.ImageStoragePool)
	planrep := dal.NewConfigPlanrep(config.Plans)
	ippool := dal.NewLibvirtIPPool(virtConn, config.Hypervisor.Network)

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
	router.Handle("/machines/{name:[^/]+}/{action:(start|stop)}/", vmango.NewHandler(ctx, handlers.MachineStateChange)).Name("machine-changestate")
	router.Handle("/machines/{name:[^/]+}/delete/", vmango.NewHandler(ctx, handlers.MachineDelete)).Name("machine-delete")
	router.Handle("/images/", vmango.NewHandler(ctx, handlers.ImageList)).Name("image-list")
	router.Handle("/ipaddress/", vmango.NewHandler(ctx, handlers.IPList)).Name("ip-list")
	router.Handle("/plans/", vmango.NewHandler(ctx, handlers.PlanList)).Name("plan-list")

	router.HandleFunc("/static{name:.*}", handlers.MakeStaticHandler(config.StaticPath)).Name("static")

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(router)

	log.WithField("address", config.Listen).Info("starting server")
	log.Fatal(http.ListenAndServe(config.Listen, n))
}
