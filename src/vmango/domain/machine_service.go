package domain

import (
	"fmt"
)

type ProviderFactory func(*ProviderConfig) (*Provider, error)

type MachineService struct {
	providerConfigs ProviderConfigrep
	providerFactory ProviderFactory
	sshkeys         SSHKeyrep
	plans           Planrep
	providersCache  map[string]*Provider
}

func NewMachineService(providerConfigs ProviderConfigrep, providerFactory ProviderFactory, sshkeys SSHKeyrep, plans Planrep) *MachineService {
	return &MachineService{
		sshkeys:         sshkeys,
		plans:           plans,
		providerConfigs: providerConfigs,
		providerFactory: providerFactory,
		providersCache:  map[string]*Provider{},
	}
}

func (s *MachineService) getProvider(name string) (*Provider, error) {
	providerConfig, err := s.providerConfigs.Get(name)
	if err != nil {
		return nil, fmt.Errorf("not found")
	}
	cacheKey := providerConfig.Hash()
	if provider, exist := s.providersCache[cacheKey]; exist {
		return provider, nil
	}
	provider, err := s.providerFactory(providerConfig)
	if err != nil {
		return nil, err
	}
	s.providersCache[cacheKey] = provider
	return provider, nil
}

func (s *MachineService) listProviders() ([]*Provider, error) {
	names, err := s.providerConfigs.ListIds()
	if err != nil {
		return nil, err
	}
	providers := []*Provider{}
	for _, name := range names {
		provider, err := s.getProvider(name)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize provider '%s': %s", name, err)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func (s *MachineService) ListProviders() ([]string, error) {
	return s.providerConfigs.ListIds()
}

type MachineCreateParams struct {
	Provider string
	Name     string
	Plan     string
	Image    string
	SSHKeys  []string
	Userdata string
	Creator  string
}

func (s *MachineService) CreateMachine(params MachineCreateParams) (*VirtualMachine, error) {
	provider, err := s.getProvider(params.Provider)
	if err != nil {
		return nil, fmt.Errorf(`failed to find provider "%s": %s`, params.Provider, err)
	}

	plan := &Plan{Name: params.Plan}
	if exists, err := s.plans.Get(plan); err != nil {
		return nil, fmt.Errorf("failed to fetch plan: %s", err)
	} else if !exists {
		return nil, fmt.Errorf(`plan "%s" not found`, params.Plan)
	}

	sshkeys := []*SSHKey{}
	for _, keyName := range params.SSHKeys {
		key := &SSHKey{Name: keyName}
		if exists, err := s.sshkeys.Get(key); err != nil {
			return nil, fmt.Errorf("failed to fetch ssh key %s: %s", keyName, err)
		} else if !exists {
			return nil, fmt.Errorf("ssh key '%s' doesn't exist", keyName)
		}
		sshkeys = append(sshkeys, key)
	}

	image := &Image{Id: params.Image}
	if exists, err := provider.Images.Get(image); err != nil {
		return nil, fmt.Errorf("failed to fetch image: %s", err)
	} else if !exists {
		return nil, fmt.Errorf(`image "%s" not found on provider "%s"`, image.Id, params.Provider)
	}

	vm := &VirtualMachine{
		Name:     params.Name,
		SSHKeys:  sshkeys,
		Userdata: params.Userdata,
		Creator:  params.Creator,
	}

	if err := provider.Machines.Create(vm, image, plan); err != nil {
		return nil, fmt.Errorf("failed to create machine: %s", err)
	}
	if err := provider.Machines.Start(vm); err != nil {
		return nil, fmt.Errorf("failed to start machine: %s", err)
	}

	return vm, nil
}

func (s *MachineService) ListMachines() (map[string]VirtualMachineList, error) {
	providers, err := s.listProviders()
	if err != nil {
		return nil, err
	}

	allMachines := map[string]VirtualMachineList{}
	for _, provider := range providers {
		machines := VirtualMachineList{}
		if err := provider.Machines.List(&machines); err != nil {
			return nil, fmt.Errorf("cannot fetch status from provider '%s': %s", provider.Name, err)
		}
		allMachines[provider.Name] = machines
	}
	return allMachines, nil
}

func (s *MachineService) GetMachine(providerId, machineId string) (*VirtualMachine, error) {
	provider, err := s.getProvider(providerId)
	if err != nil {
		return nil, err
	}
	machine := &VirtualMachine{Id: machineId}
	if exist, err := provider.Machines.Get(machine); err != nil {
		return nil, err
	} else if !exist {
		return nil, fmt.Errorf("not found")
	}
	return machine, nil
}

func (s *MachineService) RemoveMachine(providerId, machineId string) error {
	provider, err := s.getProvider(providerId)
	if err != nil {
		return err
	}
	vm, err := s.GetMachine(providerId, machineId)
	if err != nil {
		return err
	}
	return provider.Machines.Remove(vm)
}

func (s *MachineService) DoAction(providerId, machineId, action string) error {
	provider, err := s.getProvider(providerId)
	if err != nil {
		return err
	}

	machine := &VirtualMachine{Id: machineId}
	if exist, err := provider.Machines.Get(machine); err != nil {
		return fmt.Errorf("failed to fetch machine: %s", err)
	} else if !exist {
		return fmt.Errorf("machine does not exist")
	}

	switch action {
	case "stop":
		return provider.Machines.Stop(machine)
	case "start":
		return provider.Machines.Start(machine)
	case "reboot":
		return provider.Machines.Reboot(machine)
	default:
		return fmt.Errorf("unknown action '%s' requested", action)
	}
}

func (s *MachineService) ListImages() (map[string]ImageList, error) {
	providers, err := s.listProviders()
	if err != nil {
		return nil, err
	}

	allImages := map[string]ImageList{}
	for _, provider := range providers {
		var images ImageList
		if err := provider.Images.List(&images); err != nil {
			return nil, fmt.Errorf("failed to fetch images from provider '%s': %s", provider.Name, err)
		}
		allImages[provider.Name] = images
	}
	return allImages, nil
}

func (s *MachineService) ListPlans() ([]*Plan, error) {
	plans := []*Plan{}
	if err := s.plans.List(&plans); err != nil {
		return nil, err
	}
	return plans, nil
}

func (s *MachineService) ListKeys() ([]*SSHKey, error) {
	sshkeys := []*SSHKey{}
	if err := s.sshkeys.List(&sshkeys); err != nil {
		return nil, err
	}
	return sshkeys, nil
}

func (s *MachineService) Status() (map[string]*StatusInfo, error) {
	providers, err := s.listProviders()
	if err != nil {
		return nil, err
	}

	statuses := map[string]*StatusInfo{}
	for _, provider := range providers {
		status := &StatusInfo{}
		if err := provider.Status.Fetch(status); err != nil {
			return nil, fmt.Errorf("failed to query provider '%s' for status: %s", provider.Name, err)
		}
		statuses[provider.Name] = status
	}
	return statuses, nil
}
