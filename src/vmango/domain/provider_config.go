package domain

import (
	"crypto/sha1"
	"fmt"
	"io"
)

type ProviderConfig struct {
	Name   string
	Type   string
	Params map[string]string
}

func (p *ProviderConfig) Hash() string {
	h := sha1.New()
	io.WriteString(h, p.Name)
	for name, value := range p.Params {
		io.WriteString(h, name+":"+value)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
