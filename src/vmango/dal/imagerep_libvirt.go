package dal

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/libvirt/libvirt-go"
	"strings"
	"vmango/models"
)

type LibvirtImagerep struct {
	pool string
	conn *libvirt.Connect
}

func NewLibvirtImagerep(conn *libvirt.Connect, name string) *LibvirtImagerep {
	return &LibvirtImagerep{pool: name, conn: conn}
}

func (repo *LibvirtImagerep) fillImage(image *models.Image, volume *libvirt.StorageVol) bool {
	volumeName, err := volume.GetName()
	if err != nil {
		log.WithField("volume", volume).Info("cannot get image name, skipping")
		return false
	}
	logger := log.WithField("volume", volumeName)

	imginfo := strings.SplitN(volumeName, "_", 3)
	if len(imginfo) != 3 {
		logger.Warning("skipping image with invalid name")
		return false
	}
	fullpath, err := volume.GetPath()
	if err != nil {
		logger.Warning("cannot get image path, skipping")
		return false
	}
	info, err := volume.GetInfo()
	if err != nil {
		logger.Warning("cannot get image path, skipping")
		return false
	}

	image.OS = imginfo[0]
	image.Size = int64(info.Allocation)
	image.FullPath = fullpath
	image.FullName = volumeName
	image.PoolName = repo.pool

	switch imginfo[1] {
	default:
		logger.Info("skipping image with unknown architecture")
		return false
	case "amd64":
		image.Arch = models.IMAGE_ARCH_X86_64
	case "i386":
		image.Arch = models.IMAGE_ARCH_X86
	}
	switch imginfo[2] {
	default:
		logger.Warning("skipping image with unknown type")
		return false
	case "raw.img":
		image.Type = models.IMAGE_FMT_RAW
	case "qcow2.img":
		image.Type = models.IMAGE_FMT_QCOW2
	}

	return true
}

func (repo *LibvirtImagerep) List(images *[]*models.Image) error {
	pool, err := repo.conn.LookupStoragePoolByName(repo.pool)
	if err != nil {
		return err
	}
	volumes, err := pool.ListAllStorageVolumes(0)
	if err != nil {
		return err
	}
	for _, volume := range volumes {
		image := &models.Image{}
		if !repo.fillImage(image, &volume) {
			continue
		}
		*images = append(*images, image)
	}
	return nil
}

func (repo *LibvirtImagerep) Get(image *models.Image) (bool, error) {
	if image.FullName == "" {
		return false, fmt.Errorf("no filename provided")
	}
	pool, err := repo.conn.LookupStoragePoolByName(repo.pool)
	if err != nil {
		return false, err
	}
	volume, err := pool.LookupStorageVolByName(image.FullName)
	if err != nil {
		if (err.(libvirt.Error)).Code == libvirt.ERR_NO_STORAGE_VOL {
			return false, nil
		}
		return false, err
	}

	if !repo.fillImage(image, volume) {
		return true, fmt.Errorf("invalid image")
	}

	return true, nil
}
