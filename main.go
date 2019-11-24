package main

import (
	"fmt"
	"os"
	"subuk/vmango/bootstrap"
	"subuk/vmango/util"

	"github.com/akamensky/argparse"
)

func main() {
	parser := argparse.NewParser("vmango", "Vmango Virtual Machine Manager")
	configFilename := parser.String("c", "config", &argparse.Options{
		Default: util.GetenvDefault("VMANGO_CONFIG", "vmango.conf"),
		Help:    "Configuration file path",
	})
	webCommand := parser.NewCommand("web", "Start web server")
	genpwCommand := parser.NewCommand("genpw", "Generate password")
	if err := parser.Parse(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	switch {
	case webCommand.Happened():
		bootstrap.Web(*configFilename)
	case genpwCommand.Happened():
		bootstrap.GenPassword()
	}
}
