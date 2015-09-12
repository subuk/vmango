package dal

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"vmango/models"
)

type LocalfsImagerep struct {
	Root string
}

func NewLocalfsImagerep(root string) *LocalfsImagerep {
	return &LocalfsImagerep{Root: root}
}

func fillLocalfsImage(image *models.Image, fileinfo os.FileInfo) bool {

	// ubuntu-14.04_x86_64_raw.img -> name: ubuntu-14.04, arch: x86_64, type: raw.img
	imginfo := strings.SplitN(fileinfo.Name(), "_", 3)

	image.Name = imginfo[0]
	image.Size = fileinfo.Size()
	image.Date = fileinfo.ModTime()
	image.Filename = fileinfo.Name()

	switch imginfo[1] {
	default:
		log.WithField("filename", fileinfo.Name()).WithField("parts", imginfo).Info("skipping unknown image architecture")
		return false
	case "amd64":
		image.Arch = models.IMAGE_ARCH_X86_64
	case "i386":
		image.Arch = models.IMAGE_ARCH_X86
	}
	switch imginfo[2] {
	default:
		log.WithField("filename", fileinfo.Name()).WithField("parts", imginfo).Info("skipping unknown image type")
		return false
	case "raw.img":
		image.Type = models.IMAGE_FMT_RAW
	}
	return true
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
		if !fillLocalfsImage(image, fileinfo) {
			continue
		}
		*images = append(*images, image)
	}
	return nil
}

func (repo *LocalfsImagerep) Get(image *models.Image) (bool, error) {
	if image.Filename == "" {
		return false, fmt.Errorf("no filename provided")
	}
	fullpath := filepath.Join(repo.Root, image.Filename)
	fileinfo, err := os.Stat(fullpath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if !fillLocalfsImage(image, fileinfo) {
		return true, fmt.Errorf("invalid image")
	}

	return true, nil
}
