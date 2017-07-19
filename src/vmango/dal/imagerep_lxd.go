package dal

import (
	"strings"
	"vmango/models"

	"github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
)

type LXDImagerep struct {
	conn lxd.ContainerServer
}

func NewLXDImagerep(conn lxd.ContainerServer) *LXDImagerep {
	return &LXDImagerep{
		conn: conn,
	}
}

func (repo *LXDImagerep) fillImage(image *models.Image, lxdAlias api.ImageAliasesEntry, lxdImage *api.Image) error {
	image.Id = lxdAlias.Name
	image.Size = uint64(lxdImage.Size)
	image.Type = models.IMAGE_FMT_LXD
	image.OS = strings.Title(strings.Replace(lxdAlias.Name, "/", "-", 1))
	image.Arch = models.ParseHWArch(lxdImage.Architecture)
	image.Date = lxdImage.CreatedAt
	return nil
}

func (repo *LXDImagerep) fetchImages() (models.ImageList, error) {
	images := models.ImageList{}
	serverImageAliases, err := repo.conn.GetImageAliases()
	if err != nil {
		return nil, err
	}
	for _, lxdAlias := range serverImageAliases {
		if lxdAlias.Name == "" {
			continue
		}
		lxdImage, _, err := repo.conn.GetImage(lxdAlias.Target)
		if err != nil {
			return nil, err
		}

		image := &models.Image{}
		if err := repo.fillImage(image, lxdAlias, lxdImage); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, nil
}

func (repo *LXDImagerep) List(needleImages *models.ImageList) error {
	images, err := repo.fetchImages()
	if err != nil {
		return err
	}
	for _, image := range images {
		*needleImages = append(*needleImages, image)
	}
	return nil
}

func (repo *LXDImagerep) Get(needle *models.Image) (bool, error) {
	images, err := repo.fetchImages()
	if err != nil {
		return true, err
	}
	for _, image := range images {
		if image.Id == needle.Id {
			*needle = *image
			return true, nil
		}
	}
	return false, nil
}
