package dal

import (
	"fmt"
	"strconv"
	"strings"
	"vmango/models"

	"github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
)

type LXDMachinerep struct {
	conn lxd.ContainerServer
}

func (repo *LXDMachinerep) parseConfigCPUs(raw string) int {
	if raw == "" {
		return 0
	}
	if cpuCount, err := strconv.Atoi(raw); err == nil {
		return cpuCount
	}
	if strings.Contains(raw, ",") {
		cpuCount := 0
		for _, rawPart := range strings.Split(raw, ",") {
			rawPart := strings.TrimSpace(rawPart)
			if _, err := strconv.Atoi(rawPart); err == nil {
				cpuCount++
				continue
			}
			if strings.Contains(rawPart, "-") {
				cores := strings.SplitN(rawPart, "-", 2)
				startCore, err := strconv.Atoi(cores[0])
				if err != nil {
					continue
				}
				endCore, err := strconv.Atoi(cores[1])
				if err != nil {
					continue
				}
				for i := startCore; i <= endCore; i++ {
					cpuCount++
				}
			}
		}
		return cpuCount
	}
	return 0
}

func (repo *LXDMachinerep) parseState(raw api.StatusCode) int {
	switch raw {
	case api.Running:
		return models.STATE_RUNNING
	case api.Stopped:
		return models.STATE_STOPPED
	default:
		return models.STATE_UNKNOWN
	}
}

func (repo *LXDMachinerep) getIpv4(state *api.ContainerState) string {
	iface, exists := state.Network["eth0"]
	if !exists {
		return ""
	}
	for _, address := range iface.Addresses {
		if address.Family == "inet" {
			return address.Address
		}
	}
	return ""
}

func (repo *LXDMachinerep) parseConfigMemory(raw string) int {
	multiplier := 1
	rawMem := raw
	if strings.HasSuffix(raw, "kB") {
		multiplier = 1024
		rawMem = raw[:len(raw)-2]
	} else if strings.HasSuffix(raw, "MB") {
		multiplier = 1024 * 1024
		rawMem = raw[:len(raw)-2]
	} else if strings.HasSuffix(raw, "GB") {
		multiplier = 1024 * 1024 * 1024
		rawMem = raw[:len(raw)-2]
	} else if strings.HasSuffix(raw, "TB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		rawMem = raw[:len(raw)-2]
	} else if strings.HasSuffix(raw, "EB") {
		multiplier = 1024 * 1024 * 1024 * 1024 * 1024
		rawMem = raw[:len(raw)-2]
	}
	if mem, err := strconv.Atoi(rawMem); err == nil {
		return mem * multiplier
	}
	return 0
}

func (repo *LXDMachinerep) fillVm(vm *models.VirtualMachine, lxdCt api.Container, state *api.ContainerState) error {
	vm.Id = lxdCt.Name
	vm.Name = lxdCt.Name
	vm.Cpus = repo.parseConfigCPUs(lxdCt.Config["limits.cpu"])
	vm.Memory = repo.parseConfigMemory(lxdCt.Config["limits.memory"])
	vm.RootDisk = &models.VirtualMachineDisk{}
	vm.State = repo.parseState(lxdCt.StatusCode)

	vm.Ip = &models.IP{
		Address: repo.getIpv4(state),
	}
	return nil
}

func (repo *LXDMachinerep) List(vms *models.VirtualMachineList) error {
	containers, err := repo.conn.GetContainers()
	if err != nil {
		return err
	}

	for _, container := range containers {
		vm := &models.VirtualMachine{}
		state, _, err := repo.conn.GetContainerState(container.Name)
		if err != nil {
			return fmt.Errorf("failed to fetch container state: %s", err)
		}
		if err := repo.fillVm(vm, container, state); err != nil {
			return fmt.Errorf("failed to fill container: %s", err)
		}
		vms.Add(vm)
	}
	return nil
}

func (repo *LXDMachinerep) Get(vm *models.VirtualMachine) (bool, error) {
	lxdCt, _, err := repo.conn.GetContainer(vm.Id)
	if err != nil {
		return false, fmt.Errorf("failed to fetch container: %s", err)
	}
	state, _, err := repo.conn.GetContainerState(vm.Id)
	if err != nil {
		return false, fmt.Errorf("failed to fetch container state: %s", err)
	}
	if err := repo.fillVm(vm, *lxdCt, state); err != nil {
		return true, fmt.Errorf("failed to fetch container info: %s", err)
	}
	return true, nil
}

func (repo *LXDMachinerep) Create(vm *models.VirtualMachine, image *models.Image, plan *models.Plan) error {

	vm.OS = image.OS
	vm.Arch = image.Arch
	vm.Memory = plan.Memory
	vm.Cpus = plan.Cpus
	vm.ImageId = image.Id
	vm.Plan = plan.Name

	vendorData := "#cloud-config\n"
	vendorData += "ssh_authorized_keys:\n"
	for _, key := range vm.SSHKeys {
		vendorData += "  - " + key.Public + "\n"
	}

	op, err := repo.conn.CreateContainer(api.ContainersPost{
		Name: vm.Name,
		Source: api.ContainerSource{
			Type:  "image",
			Alias: image.Id,
		},
		ContainerPut: api.ContainerPut{
			Config: map[string]string{
				"user.user-data":   vm.Userdata,
				"user.vendor-data": vendorData,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("container create request failed: %s", err)
	}

	if err = op.Wait(); err != nil {
		return fmt.Errorf("container create failed: %s", err)
	}

	lxdCt, _, err := repo.conn.GetContainer(vm.Name)
	if err != nil {
		return fmt.Errorf("failed to fetch container info after create: %s", err)
	}

	state, _, err := repo.conn.GetContainerState(vm.Name)
	if err != nil {
		return fmt.Errorf("failed to fetch container state after create: %s", err)
	}

	if err := repo.fillVm(vm, *lxdCt, state); err != nil {
		return fmt.Errorf("failed to parse container info: %s", err)
	}

	return nil
}

func (repo *LXDMachinerep) updateState(vm *models.VirtualMachine, action string) error {
	req := api.ContainerStatePut{
		Action:  action,
		Timeout: -1,
	}
	op, err := repo.conn.UpdateContainerState(vm.Name, req, "")
	if err != nil {
		return fmt.Errorf("failed to make %s request for container: %s", action, err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("failed to %s container: %s", action, err)
	}
	return nil
}

func (repo *LXDMachinerep) Start(vm *models.VirtualMachine) error {
	return repo.updateState(vm, "start")
}

func (repo *LXDMachinerep) Stop(vm *models.VirtualMachine) error {
	return repo.updateState(vm, "stop")
}

func (repo *LXDMachinerep) Reboot(vm *models.VirtualMachine) error {
	return repo.updateState(vm, "restart")
}

func (repo *LXDMachinerep) Remove(vm *models.VirtualMachine) error {
	if vm.IsRunning() {
		if err := repo.Stop(vm); err != nil {
			return fmt.Errorf("failed to stop container before deletion: %s", err)
		}
	}
	op, err := repo.conn.DeleteContainer(vm.Id)
	if err != nil {
		return fmt.Errorf("failed to make container deletion request: %s", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("container deletion failed: %s", err)
	}
	return nil
}

func (repo *LXDMachinerep) ServerInfo(serverInfoList *models.ServerList) error {
	return fmt.Errorf("not implemented")
}
