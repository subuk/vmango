package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"subuk/vmango/compute"

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

// Adopt requires an explicit profile through AdoptWithPolicy. The legacy entry
// point cannot infer hardware policy or normalize an external guest.
func (repo *VirtualMachineRepository) Adopt(id, node string, m *compute.MachineMetadata) error {
	return fmt.Errorf("adoption requires an explicit compatible hardware profile; use AdoptWithPolicy")
}
