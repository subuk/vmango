package libvirt

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"

	vir "libvirt.org/go/libvirt"
	xmlmodel "libvirt.org/go/libvirtxml"
)

const workloadNamespace = "https://vmango.org/xmlns/workload/1"

type workloadXML struct {
	XMLName   xml.Name `xml:"https://vmango.org/xmlns/workload/1 workload"`
	Version   int      `xml:"version,attr"`
	Namespace string   `xml:"namespace,attr"`
	Label     string   `xml:"label"`
}

func readWorkloadMetadata(config *xmlmodel.Domain) (compute.NamespaceID, string, error) {
	if config.Metadata == nil {
		return "", "", nil
	}
	d := xml.NewDecoder(strings.NewReader("<metadata>" + config.Metadata.XML + "</metadata>"))
	var found *workloadXML
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", fmt.Errorf("invalid VM namespace metadata: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Space != workloadNamespace || start.Name.Local != "workload" {
			continue
		}
		if found != nil {
			return "", "", fmt.Errorf("duplicate VM namespace metadata")
		}
		var data workloadXML
		if err := d.DecodeElement(&data, &start); err != nil {
			return "", "", fmt.Errorf("invalid VM namespace metadata: %w", err)
		}
		if data.Version != 1 || !compute.ValidCatalogID(data.Namespace) || !compute.ValidResourceLabel(data.Label) {
			return "", "", fmt.Errorf("invalid or unsupported VM namespace metadata")
		}
		found = &data
	}
	if found == nil {
		return "", "", nil
	}
	return compute.NamespaceID(found.Namespace), found.Label, nil
}

func workloadElement(namespace compute.NamespaceID, label string) (string, error) {
	if !compute.ValidCatalogID(string(namespace)) || !compute.ValidResourceLabel(label) {
		return "", fmt.Errorf("%w: valid namespace and display label are required", compute.ErrInvalid)
	}
	data, err := xml.Marshal(workloadXML{Version: 1, Namespace: string(namespace), Label: label})
	return string(data), err
}

func writeWorkloadMetadata(config *xmlmodel.Domain, namespace compute.NamespaceID, label string) error {
	if namespace == "" {
		return nil
	}
	data, err := workloadElement(namespace, label)
	if err != nil {
		return err
	}
	var preserved strings.Builder
	if config.Metadata != nil {
		original := config.Metadata.XML
		d := xml.NewDecoder(strings.NewReader(original))
		for {
			start := d.InputOffset()
			token, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if element, ok := token.(xml.StartElement); ok {
				if err := d.Skip(); err != nil {
					return err
				}
				if element.Name.Space == workloadNamespace && element.Name.Local == "workload" {
					continue
				}
			}
			preserved.WriteString(original[start:d.InputOffset()])
		}
	}
	config.Metadata = &xmlmodel.DomainMetadata{XML: preserved.String() + data}
	return nil
}

func domainWorkload(domain *vir.Domain) (compute.NamespaceID, string, error) {
	description, err := domain.GetXMLDesc(vir.DOMAIN_XML_INACTIVE)
	if err != nil {
		return "", "", repositoryError(err, "read persistent namespace configuration")
	}
	var config xmlmodel.Domain
	if err := config.Unmarshal(description); err != nil {
		return "", "", repositoryError(err, "parse persistent namespace configuration")
	}
	return readWorkloadMetadata(&config)
}

func (repo *VirtualMachineRepository) GetNamespace(id, node string) (compute.NamespaceID, string, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return "", "", err
	}
	defer repo.pool.Release(node)
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return "", "", repositoryError(err, "lookup namespace target")
	}
	defer domain.Free()
	return domainWorkload(domain)
}

// AssignNamespace changes only persistent metadata. It never redefines domain
// XML or changes live devices, scheduler metadata, power policy, or ownership.
func (repo *VirtualMachineRepository) AssignNamespace(id, node string, namespace compute.NamespaceID, label string) (err error) {
	ref := compute.VMRef(node, id)
	var effects []tasks.Effect
	defer func() { err = mutationError(err, "domain.assign-namespace", ref, effects) }()
	element, err := workloadElement(namespace, label)
	if err != nil {
		return err
	}
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(node)
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "lookup namespace target")
	}
	defer domain.Free()
	old, oldLabel, err := domainWorkload(domain)
	if err != nil {
		return err
	}
	if old != "" && old != namespace {
		return fmt.Errorf("%w: VM is already assigned to another namespace", compute.ErrConflict)
	}
	if old == namespace && oldLabel == label {
		return nil
	}
	if err := domain.SetMetadata(vir.DOMAIN_METADATA_ELEMENT, element, "vmango", workloadNamespace, vir.DOMAIN_AFFECT_CONFIG); err != nil {
		return repositoryError(err, "persist VM namespace")
	}
	effects = append(effects, confirmed(ref, "namespace-assigned"))
	actual, actualLabel, err := domainWorkload(domain)
	if err != nil {
		return err
	}
	if actual != namespace || actualLabel != label {
		return fmt.Errorf("VM namespace persistence verification failed")
	}
	return nil
}

// NamespaceBackupXML returns secure persistent XML for operator-only backups.
// The result contains private data and must never be exposed through public DTOs.
func (repo *VirtualMachineRepository) NamespaceBackupXML(id, node string) (string, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return "", err
	}
	defer repo.pool.Release(node)
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return "", repositoryError(err, "lookup namespace backup target")
	}
	defer domain.Free()
	return domain.GetXMLDesc(vir.DOMAIN_XML_INACTIVE | vir.DOMAIN_XML_SECURE)
}
