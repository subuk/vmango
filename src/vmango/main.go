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
	text_template "text/template"
	"vmango/cfg"
	"vmango/dal"
	"vmango/web"
	vmango_router "vmango/web/router"
)

var (
	CONFIG_PATH = flag.String("config", "vmango.conf", "Path to configuration file")
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

	ctx := &web.Context{
		Logger: log.New(),
	}
	ctx.Router = vmango_router.New(config.StaticPath, ctx)
	ctx.Render = web.NewRenderer(config.TemplatePath, ctx)

	vmtpl, err := text_template.ParseFiles(config.Hypervisor.VmTemplate)
	if err != nil {
		log.WithError(err).WithField("filename", config.Hypervisor.VmTemplate).Fatal("failed to parse machine template")
	}
	voltpl, err := text_template.ParseFiles(config.Hypervisor.VolTemplate)
	if err != nil {
		log.WithError(err).WithField("filename", config.Hypervisor.VmTemplate).Fatal("failed to parse volume template")
	}

	virtConn, err := libvirt.NewConnect(config.Hypervisor.Url)
	if err != nil {
		log.WithError(err).Fatal("failed to connect to libvirt")
	}

	machines, err := dal.NewLibvirtMachinerep(
		virtConn, vmtpl, voltpl, config.Hypervisor.Network,
		config.Hypervisor.RootStoragePool, config.Hypervisor.IgnoreVms,
	)
	if err != nil {
		log.WithError(err).Fatal("failed to initialize libvirt-kvm machines")
	}

	imagerep := dal.NewLibvirtImagerep(virtConn, config.Hypervisor.ImageStoragePool)
	planrep := dal.NewConfigPlanrep(config.Plans)
	ippool := dal.NewLibvirtIPPool(virtConn, config.Hypervisor.Network)
	sshkeyrep := dal.NewConfigSSHKeyrep(config.SSHKeys)
	authrep := dal.NewConfigAuthrep(config.Users)

	ctx.Machines = machines
	ctx.Images = imagerep
	ctx.IPPool = ippool
	ctx.Plans = planrep
	ctx.SSHKeys = sshkeyrep
	ctx.AuthDB = authrep
	ctx.SessionStore = sessions.NewCookieStore([]byte(config.SessionSecret))

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(ctx.Router)

	log.WithField("address", config.Listen).Info("starting server")
	log.Fatal(http.ListenAndServe(config.Listen, n))
}
