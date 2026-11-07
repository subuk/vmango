package main

import (
	"fmt"
	"os"
	"strings"
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
	installCommand := parser.NewCommand("install", "Review and prepare Linux libvirt hosts in a temporary web wizard")
	richtBinary := installCommand.String("", "richt", &argparse.Options{Help: "Path to the compatible local Richt executable (otherwise PATH)"})
	installState := installCommand.String("", "state-dir", &argparse.Options{Help: "Private installer recovery directory"})
	installListen := installCommand.String("", "listen", &argparse.Options{Default: "127.0.0.1:0", Help: "Temporary loopback listener address"})
	installParallelism := installCommand.Int("", "parallelism", &argparse.Options{Default: 3, Help: "Maximum parallel hosts (1..8)"})
	genpwCommand := parser.NewCommand("genpw", "Generate password")
	migrateCommand := parser.NewCommand("namespace-migrate", "Preview or apply an explicit namespace migration while the server is stopped")
	mapping := migrateCommand.String("", "mapping", &argparse.Options{Required: true, Help: "Explicit migration mapping JSON file"})
	apply := migrateCommand.Flag("", "apply", &argparse.Options{Help: "Apply the mapping after a durable backup; omitted means dry-run"})
	backup := migrateCommand.String("", "backup", &argparse.Options{Help: "Absolute private backup directory, required for apply and reused to resume"})
	recoveryCommand := parser.NewCommand("task-recover", "Inspect or recover task history while the server is stopped")
	action := recoveryCommand.String("", "action", &argparse.Options{Default: "inspect", Help: "inspect, cancel or release"})
	taskID := recoveryCommand.String("", "id", &argparse.Options{Help: "Task UUID; omit to inspect all history"})
	operator := recoveryCommand.String("", "operator", &argparse.Options{Help: "Operator audit identity for cancel or release"})
	reason := recoveryCommand.String("", "reason", &argparse.Options{Help: "Audited recovery reason; required for cancel or release"})
	if err := parser.Parse(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	switch {
	case installCommand.Happened():
		explicit := false
		for _, arg := range os.Args[1:] {
			if arg == "--config" || arg == "-c" || strings.HasPrefix(arg, "--config=") {
				explicit = true
			}
		}
		if !explicit || *configFilename == "" {
			fmt.Fprintln(os.Stderr, "Install requires an explicit --config path.")
			os.Exit(1)
		}
		if err := bootstrap.Install(*configFilename, *richtBinary, *installState, *installListen, *installParallelism, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Installer error: %s\n", err)
			os.Exit(1)
		}
	case webCommand.Happened():
		if err := bootstrap.Web(*configFilename); err != nil {
			fmt.Fprintf(os.Stderr, "Web server error: %s\n", err)
			os.Exit(1)
		}
	case genpwCommand.Happened():
		bootstrap.GenPassword()
	case migrateCommand.Happened():
		if err := bootstrap.NamespaceMigrate(*configFilename, *mapping, *backup, *apply, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Namespace migration error: %s\n", err)
			os.Exit(1)
		}
	case recoveryCommand.Happened():
		if err := bootstrap.NamespaceTaskRecovery(*configFilename, *action, *taskID, *operator, *reason, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Task recovery error: %s\n", err)
			os.Exit(1)
		}
	}
}
