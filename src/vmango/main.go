package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"vmango/cfg"
	"vmango/dal"
	"vmango/handlers"
	"vmango/web"
	vmango_router "vmango/web/router"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/negroni"
	"github.com/gorilla/csrf"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

var (
	CONFIG_PATH  = flag.String("config", "vmango.conf", "Path to configuration file")
	CHECK_CONFIG = flag.Bool("check", false, "Validate configuration file and exit")
	LOG_LEVEL    = flag.String("loglevel", "info", "Log level. One of panic,fatal,error,warn,info,debug")
	VERSION      string
)

func main() {
	flag.Parse()
	logLevel, err := logrus.ParseLevel(*LOG_LEVEL)

	if err != nil {
		logrus.WithError(err).Fatal("failed to parse loglevel")
	}
	logrus.SetLevel(logLevel)

	if flag.Arg(0) == "genpw" {
		plainpw := flag.Arg(1)
		if plainpw == "" || plainpw == "--help" || plainpw == "-h" {
			logrus.Fatal("Usage: vmango genpw <password>")
			return
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(plainpw), bcrypt.DefaultCost)
		if err != nil {
			logrus.WithError(err).Fatal("failed to generate hash")
			return
		}
		fmt.Println(string(hashed))
		return
	}

	config, err := cfg.ParseConfig(*CONFIG_PATH)
	if err != nil {
		logrus.WithError(err).WithField("filename", *CONFIG_PATH).Fatal("failed to parse config")
	}
	if err := config.Sanitize(filepath.Dir(*CONFIG_PATH)); err != nil {
		fmt.Fprintf(os.Stderr, "config validation failed, %s\n", err)
		os.Exit(1)
	}
	staticCache, err := time.ParseDuration(config.StaticCache)
	if err != nil {
		logrus.WithError(err).Fatal("failed to parse static_cache from config")
	}
	if *CHECK_CONFIG {
		os.Exit(0)
	}
	ctx := &web.Context{
		Logger:      logrus.StandardLogger(),
		StaticCache: staticCache,
	}

	csrfErrorHandler := web.NewHandler(ctx, handlers.CSRFFailed)
	csrfOptions := []csrf.Option{
		csrf.FieldName("csrf"),
		csrf.ErrorHandler(csrfErrorHandler),
	}
	if !config.IsTLS() {
		csrfOptions = append(csrfOptions, csrf.Secure(false))
	}
	csrfProtect := csrf.Protect([]byte(config.SessionSecret), csrfOptions...)

	ctx.Router = vmango_router.New(ctx, csrfProtect)
	ctx.Render = web.NewRenderer(VERSION, config.Debug, ctx)

	providers := dal.Providers{}

	for _, hConfig := range config.Hypervisors {
		provider, err := dal.NewLibvirtProvider(hConfig)
		if err != nil {
			logrus.WithError(err).WithField("provider", hConfig.Name).Warning("failed to initialize libvirt hypervisor")
			continue
		}
		providers.Add(provider)
	}

	for _, aConfig := range config.AWSConnections {
		provider := dal.NewAWSProvider(aConfig)
		providers.Add(provider)
	}

	planrep := dal.NewConfigPlanrep(config.Plans)
	sshkeyrep := dal.NewConfigSSHKeyrep(config.SSHKeys)
	authrep := dal.NewConfigAuthrep(config.Users)

	ctx.Providers = providers
	ctx.Plans = planrep
	ctx.SSHKeys = sshkeyrep
	ctx.AuthDB = authrep
	ctx.SessionStore = sessions.NewCookieStore([]byte(config.SessionSecret))

	n := negroni.New()
	n.Use(web.NewLogRequestMiddleware(
		config.TrustedProxies,
		[]string{"/static/"},
	))
	n.Use(negroni.NewRecovery())
	n.UseHandler(ctx.Router)

	logrus.WithFields(logrus.Fields{
		"version": VERSION,
		"address": config.Listen,
		"tls":     config.IsTLS(),
		"debug":   config.Debug,
	}).Info("starting server")

	if config.IsTLS() {
		logrus.Fatal(http.ListenAndServeTLS(config.Listen, config.SSLCert, config.SSLKey, n))
	} else {
		logrus.Fatal(http.ListenAndServe(config.Listen, n))
	}
}
