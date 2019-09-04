package configdrive

import (
	"encoding/json"
)

type Openstack struct {
	Metadata OpenstackMetadata
	Userdata []byte
}

func (data *Openstack) Hostname() string {
	return data.Metadata.Hostname
}

func (data *Openstack) PublicKeys() []string {
	keys := []string{}
	for _, value := range data.Metadata.PublicKeys {
		keys = append(keys, value)
	}
	return keys
}

type OpenstackMetadata struct {
	Az          string            `json:"availability_zone"`
	Files       []struct{}        `json:"files"`
	Hostname    string            `json:"hostname"`
	LaunchIndex uint              `json:"launch_index"`
	Name        string            `json:"name"`
	Meta        map[string]string `json:"meta"`
	PublicKeys  map[string]string `json:"public_keys"`
	UUID        string            `json:"uuid"`
}

func (md *OpenstackMetadata) Unmarshal(in []byte) error {
	return json.Unmarshal(in, md)
}

func (md *OpenstackMetadata) Marshal() ([]byte, error) {
	return json.Marshal(md)
}
