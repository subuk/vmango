package configdrive

import (
	"gopkg.in/yaml.v2"
)

type NoCloud struct {
	Metadata NoCloudMetadata
	Userdata []byte
}

func (data *NoCloud) Hostname() string {
	return data.Metadata.Hostname
}

func (data *NoCloud) PublicKeys() []string {
	return data.Metadata.PublicKeys
}

type NoCloudMetadata struct {
	InstanceId    string   `yaml:"instance-id"`
	Hostname      string   `yaml:"hostname"`
	LocalHostname string   `yaml:"local-hostname"`
	PublicKeys    []string `yaml:"public-keys"`
}

func (md *NoCloudMetadata) Unmarshal(in []byte) error {
	return yaml.Unmarshal(in, md)
}

func (md *NoCloudMetadata) Marshal() ([]byte, error) {
	return yaml.Marshal(md)
}
