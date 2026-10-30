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
		d := xml.NewDecoder(strings.NewReader("<metadata>" + config.Metadata.XML + "</metadata>"))
		enc := xml.NewEncoder(&preserved)
		depth := 0
		for {
			token, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			switch t := token.(type) {
			case xml.StartElement:
				if depth == 1 && t.Name.Space == schedulerNamespace && t.Name.Local == "allocation" {
					if err = d.Skip(); err != nil {
						return err
					}
					continue
				}
				depth++
				if depth == 1 {
					continue
				}
			case xml.EndElement:
				depth--
				if depth == 0 {
					continue
				}
			}
			if err = enc.EncodeToken(token); err != nil {
				return err
			}
		}
		if err := enc.Flush(); err != nil {
			return err
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

// Adopt changes only our namespaced metadata, including for a running domain.
func (repo *VirtualMachineRepository) Adopt(id, node string, m *compute.MachineMetadata) error {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(node)
	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return repositoryError(err, "lookup adoption target")
	}
	defer domain.Free()
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	element, err := xml.Marshal(schedulerXML{Version: 1, Snapshot: string(data)})
	if err != nil {
		return err
	}
	return repositoryError(domain.SetMetadata(vir.DOMAIN_METADATA_ELEMENT, string(element), "vmango", schedulerNamespace, vir.DOMAIN_AFFECT_CONFIG), "write adoption metadata")
}
