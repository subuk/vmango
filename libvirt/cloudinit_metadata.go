package libvirt

import (
	"gopkg.in/yaml.v2"
)

type CloudInitMetadata struct {
	InstanceId    string   `yaml:"instance-id"`
	Hostname      string   `yaml:"hostname"`
	LocalHostname string   `yaml:"local-hostname"`
	PublicKeys    []string `yaml:"public-keys"`
}

func (md *CloudInitMetadata) Unmarshal(in []byte) error {
	return yaml.Unmarshal(in, md)
}

func (md *CloudInitMetadata) Marshal() ([]byte, error) {
	return yaml.Marshal(md)
}
