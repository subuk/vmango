package compute

type NetworkRepository interface {
	List(options NetworkListOptions) ([]*Network, error)
	Get(name, node string) (*Network, error)
}

type Network struct {
	NodeId string
	Name   string
	Type   NetworkType
}
