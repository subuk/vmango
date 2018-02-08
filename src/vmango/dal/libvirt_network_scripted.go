package dal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"vmango/domain"
)

type LibvirtScriptedNetwork struct {
	scriptPath  string
	networkName string
}

func NewLibvirtScriptedNetwork(networkName, scriptPath string) *LibvirtScriptedNetwork {
	return &LibvirtScriptedNetwork{
		scriptPath:  scriptPath,
		networkName: networkName,
	}
}

func (backend *LibvirtScriptedNetwork) Name() string {
	return backend.networkName
}

func (backend *LibvirtScriptedNetwork) getCommand(vm *domain.VirtualMachine, args ...string) *exec.Cmd {
	cmd := exec.Command(backend.scriptPath, args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "VMANGO_MACHINE_HWADDR="+vm.HWAddr)
	cmd.Env = append(cmd.Env, "VMANGO_NETWORK_NAME="+backend.networkName)
	cmd.Env = append(cmd.Env, "VMANGO_MACHINE_NAME="+vm.Name)
	cmd.Env = append(cmd.Env, "VMANGO_MACHINE_PLAN="+vm.Plan)
	if vm.Ip != nil {
		cmd.Env = append(cmd.Env, "VMANGO_MACHINE_IP="+vm.Ip.Address)
	}
	cmd.Env = append(cmd.Env, "VMANGO_MACHINE_ID="+vm.Id)
	return cmd
}

func (backend *LibvirtScriptedNetwork) getOutput(vm *domain.VirtualMachine, args ...string) (string, error) {
	cmd := backend.getCommand(vm, args...)
	rawOutput, err := cmd.Output()
	if err != nil {
		errput := ""
		if ee, ok := err.(*exec.ExitError); ok {
			errput = string(ee.Stderr)
		}
		return "", fmt.Errorf("command '%s' failed: %s. %s", cmd.Args, err, errput)
	}
	output := strings.TrimSpace(string(rawOutput))
	if output == "" {
		return "", fmt.Errorf("script '%s' did not return anything", cmd.Args)
	}
	return output, nil
}

func (backend *LibvirtScriptedNetwork) ReleaseIP(vm *domain.VirtualMachine) error {
	return backend.getCommand(vm, "release-ip").Run()
}

func (backend *LibvirtScriptedNetwork) LookupIP(vm *domain.VirtualMachine) error {
	address, err := backend.getOutput(vm, "lookup-ip")
	if err != nil {
		return err
	}
	if vm.Ip == nil {
		vm.Ip = &domain.IP{}
	}
	vm.Ip.Address = address
	return nil
}

func (backend *LibvirtScriptedNetwork) AssignIP(vm *domain.VirtualMachine) error {
	address, err := backend.getOutput(vm, "assign-ip")
	if err != nil {
		return err
	}
	vm.Ip.Address = address
	return nil
}
