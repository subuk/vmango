package compute

import (
	"errors"
)

var ErrUnknownNode = errors.New("unknown node")

type NodeGetOptions struct {
	CpuNumaIdFilter bool
	CpuNumaId       int
	NoPins          bool
}

type NodeListOptions struct {
	NoPins bool
}

type NodeRepository interface {
	Get(node string, options NodeGetOptions) (*Node, error)
	List(options NodeListOptions) ([]*Node, error)
}

type NodeService struct {
	NodeRepository
}

func NewNodeService(repo NodeRepository) *NodeService {
	return &NodeService{repo}
}
