package main

import (
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/sessions"
	"github.com/libvirt/libvirt-go"
	"github.com/meatballhat/negroni-logrus"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"os"
	"path/filepath"
	text_template "text/template"
	"time"
	"vmango/cfg"
	"vmango/dal"
	"vmango/web"
	vmango_router "vmango/web/router"
)

var (
	CONFIG_PATH    = flag.String("config", "vmango.conf", "Path to configuration file")
	CHECK_CONFIG   = flag.Bool("check", false, "Validate configuration file and exit")
	STATIC_VERSION string
)

func main() {
	flag.Parse()
	log.SetLevel(log.InfoLevel)

	if flag.Arg(0) == "genpw" {
		plainpw := flag.Arg(1)
		if plainpw == "" || plainpw == "--help" || plainpw == "-h" {
			log.Fatal("Usage: vmango genpw <password>")
			return
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(plainpw), bcrypt.DefaultCost)
		if err != nil {
			log.WithError(err).Fatal("failed to generate hash")
			return
		}
		fmt.Println(string(hashed))
		return
	}

	config, err := cfg.ParseConfig(*CONFIG_PATH)
	if err != nil {
		log.WithError(err).WithField("filename", *CONFIG_PATH).Fatal("failed to parse config")
	}
	if err := config.Sanitize(filepath.Dir(*CONFIG_PATH)); err != nil {
		fmt.Fprintf(os.Stderr, "config validation failed, %s\n", err)
		os.Exit(1)
	}
	staticCache, err := time.ParseDuration(config.StaticCache)
	if err != nil {
		log.WithError(err).Fatal("failed to parse static_cache from config")
	}
	if *CHECK_CONFIG {
		os.Exit(0)
	}
	ctx := &web.Context{
		Logger:      log.New(),
		StaticCache: staticCache,
	}
	ctx.Router = vmango_router.New(ctx)
	staticVersion := STATIC_VERSION
	if config.Debug {
		staticVersion = ""
	}
	ctx.Render = web.NewRenderer(staticVersion, config.Debug, ctx)

	hypervisors := dal.HypervisorList{}

	for _, hConfig := range config.Hypervisors {
		vmtpl, err := text_template.ParseFiles(hConfig.VmTemplate)
		if err != nil {
			log.WithError(err).WithField("hypervisor", hConfig.Name).WithField("filename", hConfig.VmTemplate).Fatal("failed to parse machine template")
		}
		voltpl, err := text_template.ParseFiles(hConfig.VolTemplate)
		if err != nil {
			log.WithError(err).WithField("hypervisor", hConfig.Name).WithField("filename", hConfig.VmTemplate).Fatal("failed to parse volume template")
		}
		virtConn, err := libvirt.NewConnect(hConfig.Url)
		if err != nil {
			log.WithError(err).WithField("hypervisor", hConfig.Name).Fatal("failed to connect to libvirt")
		}
		machinerep, err := dal.NewLibvirtMachinerep(
			virtConn, vmtpl, voltpl, hConfig.Network,
			hConfig.RootStoragePool, hConfig.Name,
			hConfig.IgnoreVms,
		)
		if err != nil {
			log.WithError(err).WithField("hypervisor", hConfig.Name).Fatal("failed to initialize hypervisor")
		}
		imagerep := dal.NewLibvirtImagerep(virtConn, hConfig.ImageStoragePool, hConfig.Name)
		hypervisors.Add(&dal.Hypervisor{
			Name:     hConfig.Name,
			Machines: machinerep,
			Images:   imagerep,
		})
	}

	planrep := dal.NewConfigPlanrep(config.Plans)
	sshkeyrep := dal.NewConfigSSHKeyrep(config.SSHKeys)
	authrep := dal.NewConfigAuthrep(config.Users)

	ctx.Hypervisors = hypervisors
	ctx.Plans = planrep
	ctx.SSHKeys = sshkeyrep
	ctx.AuthDB = authrep
	ctx.SessionStore = sessions.NewCookieStore([]byte(config.SessionSecret))

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(ctx.Router)

	if config.SSLKey != "" && config.SSLCert != "" {
		log.WithField("address", config.Listen).Info("starting SSL server")
		log.Fatal(http.ListenAndServeTLS(config.Listen, config.SSLCert, config.SSLKey, n))
	} else {
		log.WithField("address", config.Listen).Info("starting server")
		log.Fatal(http.ListenAndServe(config.Listen, n))
	}

}
