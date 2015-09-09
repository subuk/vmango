package dal

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"io/ioutil"
	"strings"
	"vmango/models"
)

type LocalfsImagerep struct {
	Root string
}

func NewLocalfsImagerep(root string) *LocalfsImagerep {
	return &LocalfsImagerep{Root: root}
}

func (repo *LocalfsImagerep) List(images *[]*models.Image) error {
	files, err := ioutil.ReadDir(repo.Root)
	if err != nil {
		return err
	}
	for _, fileinfo := range files {
		if fileinfo.IsDir() {
			continue
		}

		image := &models.Image{}
		// ubuntu-14.04_x86_64_raw.img -> name: ubuntu-14.04, arch: x86_64, type: raw.img
		imginfo := strings.SplitN(fileinfo.Name(), "_", 3)

		image.Name = imginfo[0]
		image.Size = fileinfo.Size()
		image.Date = fileinfo.ModTime()

		switch imginfo[1] {
		default:
			log.WithField("filename", fileinfo.Name()).WithField("parts", imginfo).Info("skipping unknown image architecture")
			continue
		case "amd64", "x86-64":
			image.Arch = models.IMAGE_ARCH_X86_64
		case "i386", "x86":
			image.Arch = models.IMAGE_ARCH_X86
		}
		switch imginfo[2] {
		default:
			log.WithField("filename", fileinfo.Name()).WithField("parts", imginfo).Info("skipping unknown image type")
			continue
		case "raw.img":
			image.Type = models.IMAGE_FMT_RAW
		}
		*images = append(*images, image)
	}
	return nil
}

func (l *LocalfsImagerep) Get(*models.Image) (bool, error) {
	return false, fmt.Errorf("not implemeted")
}
