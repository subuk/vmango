package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"subuk/vmango/compute"

	vir "libvirt.org/go/libvirt"
	xmlmodel "libvirt.org/go/libvirtxml"
)

const schedulerNamespace = "https://vmango.org/xmlns/scheduler/1"

type schedulerXML struct {
	XMLName  xml.Name `xml:"https://vmango.org/xmlns/scheduler/1 allocation"`
	Version  int      `xml:"version,attr"`
	Snapshot string   `xml:",chardata"`
}

func readSchedulerMetadata(config *xmlmodel.Domain) (*compute.MachineMetadata, error) {
	if config.Metadata == nil {
		return nil, nil
	}
	d := xml.NewDecoder(strings.NewReader("<metadata>" + config.Metadata.XML + "</metadata>"))
	var result *compute.MachineMetadata
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Space != schedulerNamespace || start.Name.Local != "allocation" {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("duplicate scheduler metadata")
		}
		var data schedulerXML
		if err = d.DecodeElement(&data, &start); err != nil {
			return nil, err
		}
		if data.Version != 1 {
			return nil, fmt.Errorf("unsupported scheduler metadata version")
		}
		var m compute.MachineMetadata
		if err = json.Unmarshal([]byte(data.Snapshot), &m); err != nil {
			return nil, err
		}
		if m.Cloud == "" || m.Zone == "" || m.CPURequest < 1 || m.MemoryRequestBytes == 0 {
			return nil, fmt.Errorf("invalid scheduler metadata")
		}
		result = &m
	}
	return result, nil
}

func writeSchedulerMetadata(config *xmlmodel.Domain, metadata *compute.MachineMetadata) error {
	if metadata == nil {
		return nil
	}
	// Preserve unrelated metadata content, replacing only our namespaced element.
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
				if element.Name.Space == schedulerNamespace && element.Name.Local == "allocation" {
					continue
				}
			}
			// Keep unrelated namespaces and prefixes verbatim. Re-encoding tokens
			// can duplicate xmlns attributes or invalidate QName attribute values.
			preserved.WriteString(original[start:d.InputOffset()])
		}
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	data, err := xml.Marshal(schedulerXML{Version: 1, Snapshot: string(payload)})
	if err != nil {
		return err
	}
	config.Metadata = &xmlmodel.DomainMetadata{XML: preserved.String() + string(data)}
	return nil
}

// Adopt records management identity and normalizes persistent power settings.
// A running guest keeps its current hardware until its next full start.
func (repo *VirtualMachineRepository) Adopt(id, node string, m *compute.MachineMetadata) error {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(node)
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "lookup adoption target")
	}
	defer domain.Free()
	// Read the persistent secure XML so running device state and redacted
	// credentials cannot replace the configuration for the next start.
	description, err := domain.GetXMLDesc(vir.DOMAIN_XML_INACTIVE | vir.DOMAIN_XML_SECURE)
	if err != nil {
		return repositoryError(err, "read adoption configuration")
	}
	config := &xmlmodel.Domain{}
	if err := config.Unmarshal(description); err != nil {
		return repositoryError(err, "parse adoption configuration")
	}
	applyDomainPowerPolicy(config)
	if err := writeSchedulerMetadata(config, m); err != nil {
		return err
	}
	description, err = config.Marshal()
	if err != nil {
		return err
	}
	// Define metadata and power policy together; do not report a partially
	// adopted machine if validation rejects the updated configuration.
	defined, err := conn.DomainDefineXML(description)
	if err != nil {
		return repositoryError(err, "define adoption configuration")
	}
	defer defined.Free()
	return nil
}
