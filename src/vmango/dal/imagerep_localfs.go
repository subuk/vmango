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

func (repo *LocalfsImagerep) fillLocalfsImage(image *models.Image, fileinfo os.FileInfo) bool {

	// ubuntu-14.04_x86_64_raw.img -> name: ubuntu-14.04, arch: x86_64, type: raw.img
	imginfo := strings.SplitN(fileinfo.Name(), "_", 3)

	if len(imginfo) != 3 {
		log.WithField("image", fileinfo.Name()).Info("skipping image with invalid name")
		return false
	}

	image.Name = imginfo[0]
	image.Size = fileinfo.Size()
	image.Date = fileinfo.ModTime()
	image.Filename = fileinfo.Name()
	image.FullPath = filepath.Join(repo.Root, fileinfo.Name())
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
	case "qcow2.img":
		image.Type = models.IMAGE_FMT_QCOW2
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
		if !repo.fillLocalfsImage(image, fileinfo) {
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

	if !repo.fillLocalfsImage(image, fileinfo) {
		return true, fmt.Errorf("invalid image")
	}

	return true, nil
}
