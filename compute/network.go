package compute

import (
	"errors"
)

var ErrInterfaceNotFound = errors.New("interface not found")

type Network struct {
	NodeId string
	Name   string
}
