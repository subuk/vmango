package compute

type NetworkRepository interface {
	List() ([]*Network, error)
}

type Network struct {
	Name string
	Type NetworkType
}
