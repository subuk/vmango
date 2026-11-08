package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"io"
	xmlmodel "libvirt.org/go/libvirtxml"
	"strings"
	"subuk/vmango/libvirt/hardware"
)

const hardwareNamespace = "https://vmango.org/xmlns/hardware/1"

type hardwareXML struct {
	XMLName  xml.Name `xml:"https://vmango.org/xmlns/hardware/1 policy"`
	Snapshot string   `xml:",chardata"`
}

func readHardwareMetadata(config *xmlmodel.Domain) (json.RawMessage, error) {
	if config.Metadata == nil {
		return nil, nil
	}
	d := xml.NewDecoder(strings.NewReader("<metadata>" + config.Metadata.XML + "</metadata>"))
	var raw json.RawMessage
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Space != hardwareNamespace || start.Name.Local != "policy" {
			continue
		}
		if raw != nil {
			return json.RawMessage(`{"version":-1}`), nil
		}
		var data hardwareXML
		if err = d.DecodeElement(&data, &start); err != nil {
			return nil, err
		}
		raw = json.RawMessage(data.Snapshot)
	}
	return raw, nil
}
func writeHardwareMetadata(config *xmlmodel.Domain, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if _, err := hardware.DecodePlan(raw); err != nil {
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
			if e, ok := token.(xml.StartElement); ok {
				if err = d.Skip(); err != nil {
					return err
				}
				if e.Name.Space == hardwareNamespace && e.Name.Local == "policy" {
					continue
				}
			}
			preserved.WriteString(original[start:d.InputOffset()])
		}
	}
	encoded, err := xml.Marshal(hardwareXML{Snapshot: string(raw)})
	if err != nil {
		return err
	}
	config.Metadata = &xmlmodel.DomainMetadata{XML: preserved.String() + string(encoded)}
	return nil
}
