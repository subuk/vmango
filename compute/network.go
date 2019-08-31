package compute

type NetworkRepository interface {
	List() ([]*Network, error)
	Get(name string) (*Network, error)
}

type Network struct {
	Name string
	Type NetworkType
}
