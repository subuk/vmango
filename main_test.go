package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCLIProcess(t *testing.T) {
	if os.Getenv("VMANGO_TEST_CLI_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"vmango"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing argument separator")
}

func TestCLIHelpAndErrors(t *testing.T) {
	type testCase struct {
		name string
		args []string
		help bool
		want string
	}
	tests := []testCase{
		{name: "root help", args: []string{"--help"}, help: true, want: "Vmango Virtual Machine Manager"},
		{name: "short help", args: []string{"-h"}, help: true, want: "Commands:"},
		{name: "no command", want: "expected one of"},
		{name: "unknown command", args: []string{"unknown"}, want: "unexpected argument unknown"},
		{name: "unknown root flag", args: []string{"--unknown"}, want: "unknown flag --unknown"},
		{name: "unknown command flag", args: []string{"web", "--unknown"}, want: "unknown flag --unknown"},
		{name: "extra argument", args: []string{"genpw", "extra"}, want: "unexpected argument extra"},
		{name: "missing flag value", args: []string{"web", "--config"}, want: "--config"},
		{name: "missing required flag", args: []string{"namespace-migrate"}, want: "--mapping"},
		{name: "invalid number", args: []string{"install", "--parallelism", "bad"}, want: "--parallelism"},
		{name: "invalid action", args: []string{"task-recover", "--action", "bad"}, want: "--action"},
		{name: "installer explicit config", args: []string{"install"}, want: "install requires an explicit --config path"},
		{name: "installer empty config", args: []string{"install", "--config="}, want: "install requires an explicit --config path"},
	}
	for _, command := range []string{"web", "install", "genpw", "namespace-migrate", "task-recover"} {
		for _, flag := range []string{"--help", "-h"} {
			tests = append(tests, testCase{command + " " + flag, []string{command, flag}, true, "Usage: vmango " + command})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--"}, tt.args...)...)
			cmd.Env = append(os.Environ(), "VMANGO_TEST_CLI_PROCESS=1", "VMANGO_CONFIG="+t.TempDir()+"/missing.conf")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, "Usage: vmango") || !strings.Contains(output, tt.want) {
				t.Fatalf("missing usage or %q in output:\n%s", tt.want, output)
			}
			if tt.help {
				if err != nil || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Flags:") {
					t.Fatalf("help failed: %v\n%s", err, output)
				}
			} else {
				if err == nil || !strings.Contains(stderr.String(), "error:") {
					t.Fatalf("expected failure and diagnostic: %v\n%s", err, output)
				}
				if strings.Contains(output, "Flags:") || strings.Contains(output, "Commands:") {
					t.Fatalf("expected short usage, got full help:\n%s", output)
				}
			}
		})
	}
}

func TestCLIConfigAndCommands(t *testing.T) {
	for _, configDefault := range []string{"", "/environment/vmango.conf"} {
		t.Run("environment="+configDefault, func(t *testing.T) {
			t.Setenv("VMANGO_CONFIG", configDefault)
			for _, args := range [][]string{
				{"web"}, {"genpw"}, {"task-recover"},
				{"web", "-c", "/explicit.conf"},
				{"--config", "/explicit.conf", "web"},
				{"install", "--config=/explicit.conf"},
				{"-c", "/explicit.conf", "install"},
				{"namespace-migrate", "--mapping", "mapping.json", "--apply", "--backup", "/backup"},
				{"task-recover", "--action", "cancel", "--id", "task", "--operator", "admin", "--reason", "obsolete"},
			} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					var cli commandLine
					parser, err := newCLIParser(&cli)
					if err != nil {
						t.Fatal(err)
					}
					ctx, err := parser.Parse(args)
					if err != nil {
						t.Fatal(err)
					}
					want := configDefault
					if want == "" {
						want = "vmango.conf"
					}
					if strings.Contains(strings.Join(args, " "), "/explicit.conf") {
						want = "/explicit.conf"
					}
					if cli.Config != want {
						t.Fatalf("config = %q, want %q", cli.Config, want)
					}
					switch ctx.Command() {
					case "install":
						if cli.Install.Listen != "127.0.0.1:0" || cli.Install.Parallelism != 3 {
							t.Fatalf("installer defaults: %+v", cli.Install)
						}
					case "namespace-migrate":
						if cli.NamespaceMigrate.Mapping != "mapping.json" || !cli.NamespaceMigrate.Apply || cli.NamespaceMigrate.Backup != "/backup" {
							t.Fatalf("migration options: %+v", cli.NamespaceMigrate)
						}
					case "task-recover":
						if len(args) == 1 {
							if cli.TaskRecover.Action != "inspect" {
								t.Fatal("wrong default action")
							}
						} else if cli.TaskRecover.Action != "cancel" || cli.TaskRecover.ID != "task" || cli.TaskRecover.Operator != "admin" || cli.TaskRecover.Reason != "obsolete" {
							t.Fatalf("recovery options: %+v", cli.TaskRecover)
						}
					}
				})
			}
		})
	}
}
