package dal

import (
	"encoding/xml"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/libvirt/libvirt-go"
	"strconv"
	"strings"
	"time"
	"vmango/models"
)

type volumeXMLConfig struct {
	Name       string `xml:"name"`
	Allocation uint64 `xml:"allocation"`
	Target     struct {
		Path       string `xml:"path"`
		Timestamps struct {
			MTimeRaw string `xml:"mtime"`
		} `xml:"timestamps"`
		Format struct {
			Type string `xml:"type,attr"`
		} `xml:"format"`
	} `xml:"target"`
}

func (v volumeXMLConfig) LastModified() time.Time {
	parts := strings.SplitN(v.Target.Timestamps.MTimeRaw, ".", 2)
	if len(parts) == 1 {
		sec, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return time.Time{}
		}
		return time.Unix(sec, 0)
	} else if len(parts) == 2 {
		sec, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return time.Time{}
		}
		nsec, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return time.Time{}
		}
		return time.Unix(sec, nsec)
	}
	return time.Time{}

}

type LibvirtImagerep struct {
	pool       string
	hypervisor string
	conn       *libvirt.Connect
}

func NewLibvirtImagerep(conn *libvirt.Connect, name, hypervisor string) *LibvirtImagerep {
	return &LibvirtImagerep{pool: name, conn: conn, hypervisor: hypervisor}
}

func (repo *LibvirtImagerep) fillImage(image *models.Image, volume *libvirt.StorageVol) error {
	volumeXMLString, err := volume.GetXMLDesc(0)
	if err != nil {
		return err
	}
	volumeConfig := volumeXMLConfig{}
	if err := xml.Unmarshal([]byte(volumeXMLString), &volumeConfig); err != nil {
		return fmt.Errorf("failed to parse volume xml: %s", err)
	}

	imginfo := strings.SplitN(volumeConfig.Name, "_", 3)
	if len(imginfo) < 2 {
		return fmt.Errorf("invalid name")
	}

	switch imginfo[1] {
	default:
		return fmt.Errorf("unknown arch")
	case "amd64":
		image.Arch = models.IMAGE_ARCH_X86_64
	case "i386":
		image.Arch = models.IMAGE_ARCH_X86
	}
	switch volumeConfig.Target.Format.Type {
	default:
		return fmt.Errorf("unknown type")
	case "raw":
		image.Type = models.IMAGE_FMT_RAW
	case "qcow2":
		image.Type = models.IMAGE_FMT_QCOW2
	}

	image.OS = imginfo[0]
	image.Size = volumeConfig.Allocation
	image.FullPath = volumeConfig.Target.Path
	image.FullName = volumeConfig.Name
	image.PoolName = repo.pool
	image.Date = volumeConfig.LastModified()
	image.Hypervisor = repo.hypervisor
	return nil
}

func (repo *LibvirtImagerep) List(images *models.ImageList) error {
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
		if err := repo.fillImage(image, &volume); err != nil {
			log.WithError(err).Warn("skipping volume")
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

	if err := repo.fillImage(image, volume); err != nil {
		return true, fmt.Errorf("invalid image: %s", err)
	}

	return true, nil
}
