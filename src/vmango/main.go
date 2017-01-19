package main

import (
	"flag"
	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/negroni"
	"github.com/libvirt/libvirt-go"
	"github.com/meatballhat/negroni-logrus"
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
	sshkeyrep := dal.NewConfigSSHKeyrep(config.SSHKeys)

	ctx.Machines = machines
	ctx.Images = imagerep
	ctx.IPPool = ippool
	ctx.Plans = planrep
	ctx.SSHKeys = sshkeyrep

	n := negroni.New()
	n.Use(negronilogrus.NewMiddleware())
	n.Use(negroni.NewRecovery())
	n.UseHandler(ctx.Router)

	log.WithField("address", config.Listen).Info("starting server")
	log.Fatal(http.ListenAndServe(config.Listen, n))
}
