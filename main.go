package main

import (
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
	if err := parser.Parse(os.Args); err != nil {
		os.Exit(1)
	}
	bootstrap.Web(*configFilename)
}
