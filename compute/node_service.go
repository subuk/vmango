package compute

type NodeRepository interface {
	Get(node string) (*Node, error)
	List() ([]*Node, error)
}

type NodeService struct {
	NodeRepository
}

func NewNodeService(repo NodeRepository) *NodeService {
	return &NodeService{repo}
}
