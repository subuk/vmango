package compute

type VolumePoolListOptions struct {
	NodeIds []string
}

type VolumePoolRepository interface {
	List(options VolumePoolListOptions) ([]*VolumePool, error)
}

type VolumePoolService struct {
	VolumePoolRepository
}

func NewVolumePoolService(repo VolumePoolRepository) *VolumePoolService {
	return &VolumePoolService{repo}
}
