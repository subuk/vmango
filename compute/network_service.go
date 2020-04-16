package compute

type NetworkListOptions struct {
	NodeIds []string
}

type NetworkRepository interface {
	List(options NetworkListOptions) ([]*Network, error)
	Get(name, node string) (*Network, error)
}

type NetworkService struct {
	NetworkRepository
}

func NewNetworkService(repo NetworkRepository) *NetworkService {
	return &NetworkService{repo}
}
