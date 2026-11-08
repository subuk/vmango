package main

import (
	"fmt"
	"os"
	"subuk/vmango/bootstrap"
	"subuk/vmango/util"

	"github.com/alecthomas/kong"
)

type commandLine struct {
	Config  string   `short:"c" default:"${config_default}" help:"Configuration file path (defaults to VMANGO_CONFIG or vmango.conf)."`
	Web     struct{} `cmd:"" help:"Start web server."`
	Install struct {
		Richt       string `help:"Path to the compatible local Richt executable (otherwise PATH)."`
		StateDir    string `help:"Private installer recovery directory."`
		Listen      string `default:"127.0.0.1:0" help:"Temporary loopback listener address."`
		Parallelism int    `default:"3" help:"Maximum parallel hosts (1..8)."`
	} `cmd:"" help:"Review and prepare Linux libvirt hosts in a temporary web wizard. Requires an explicit --config path."`
	Genpw            struct{} `cmd:"" help:"Generate password."`
	NamespaceMigrate struct {
		Mapping string `required:"" help:"Explicit migration mapping JSON file."`
		Apply   bool   `help:"Apply the mapping after a durable backup; omitted means dry-run."`
		Backup  string `help:"Absolute private backup directory, required for apply and reused to resume."`
	} `cmd:"" help:"Preview or apply an explicit namespace migration while the server is stopped."`
	TaskRecover struct {
		Action   string `default:"inspect" enum:"inspect,cancel,release" help:"Recovery action: inspect, cancel or release."`
		ID       string `name:"id" help:"Task UUID; omit to inspect all history."`
		Operator string `help:"Operator audit identity for cancel or release."`
		Reason   string `help:"Audited recovery reason; required for cancel or release."`
	} `cmd:"" help:"Inspect or recover task history while the server is stopped."`
}

func (cli *commandLine) Validate(ctx *kong.Context) error {
	if ctx.Command() == "install" {
		for _, path := range ctx.Path {
			if path.Flag != nil && path.Flag.Name == "config" && cli.Config != "" {
				return nil
			}
		}
		return fmt.Errorf("install requires an explicit --config path")
	}
	return nil
}

func newCLIParser(cli *commandLine) (*kong.Kong, error) {
	return kong.New(cli,
		kong.Name("vmango"),
		kong.Description("Vmango Virtual Machine Manager"),
		kong.ShortUsageOnError(),
		kong.Vars{"config_default": util.GetenvDefault("VMANGO_CONFIG", "vmango.conf")},
	)
}

func main() {
	var cli commandLine
	parser, err := newCLIParser(&cli)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)
	switch ctx.Command() {
	case "install":
		if err := bootstrap.Install(cli.Config, cli.Install.Richt, cli.Install.StateDir, cli.Install.Listen, cli.Install.Parallelism, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Installer error: %s\n", err)
			os.Exit(1)
		}
	case "web":
		if err := bootstrap.Web(cli.Config); err != nil {
			fmt.Fprintf(os.Stderr, "Web server error: %s\n", err)
			os.Exit(1)
		}
	case "genpw":
		bootstrap.GenPassword()
	case "namespace-migrate":
		if err := bootstrap.NamespaceMigrate(cli.Config, cli.NamespaceMigrate.Mapping, cli.NamespaceMigrate.Backup, cli.NamespaceMigrate.Apply, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Namespace migration error: %s\n", err)
			os.Exit(1)
		}
	case "task-recover":
		if err := bootstrap.NamespaceTaskRecovery(cli.Config, cli.TaskRecover.Action, cli.TaskRecover.ID, cli.TaskRecover.Operator, cli.TaskRecover.Reason, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Task recovery error: %s\n", err)
			os.Exit(1)
		}
	}
}
