package dal

import (
	"fmt"
	"strings"
	"text/template"
	"vmango/domain"

	libvirt "github.com/libvirt/libvirt-go"
)

func LibvirtProviderFactory(pc *domain.ProviderConfig) (*domain.Provider, error) {
	vmtpl, err := template.New(pc.Name + "-machine-template").Parse(pc.Params["machine_template"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse machine template for %s: %s", pc.Name, err)
	}
	voltpl, err := template.New(pc.Name + "-volume-template").Parse(pc.Params["volume_template"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse volume template for %s: %s", pc.Name, err)
	}
	virtConn, err := libvirt.NewConnect(pc.Params["url"])
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt '%s': %s", pc.Params["url"], err)
	}

	ignoreVms := []string{}
	for _, ignoreVm := range strings.Split(pc.Params["ignore_vms"], ",") {
		ignoreVms = append(ignoreVms, strings.TrimSpace(ignoreVm))
	}

	var network LibvirtNetwork
	if pc.Params["network_script"] != "" {
		network = NewLibvirtScriptedNetwork(pc.Params["network"], pc.Params["network_script"])
	} else if pc.Params["network_script"] == "" && pc.Params["network"] != "" {
		network = NewLibvirtManagedNetwork(virtConn, pc.Params["network"])
	} else {
		return nil, fmt.Errorf("no network or network_script specified")
	}

	machinerep := NewLibvirtMachinerep(virtConn, vmtpl, voltpl, network, pc.Params["root_storage_pool"], ignoreVms)
	imagerep := NewLibvirtImagerep(virtConn, pc.Params["image_storage_pool"])
	statusrep := NewLibvirtStatusrep(virtConn, pc.Params["root_storage_pool"])
	provider := &domain.Provider{
		Name:     pc.Name,
		Machines: machinerep,
		Images:   imagerep,
		Status:   statusrep,
	}
	return provider, nil
}
