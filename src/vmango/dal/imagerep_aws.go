package dal

import (
	"fmt"
	"time"
	"vmango/cfg"
	"vmango/models"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	aws_ec2 "github.com/aws/aws-sdk-go/service/ec2"
)

type AWSImagerep struct {
	conf []cfg.AWSImageConfig
	ec2  *aws_ec2.EC2
}

func (repo *AWSImagerep) fetchImages() (models.ImageList, error) {
	images := models.ImageList{}
	for _, imconf := range repo.conf {
		images = append(images, &models.Image{
			Id: imconf.Id,
			OS: imconf.OS,
		})
	}
	awsInfo, err := repo.ec2.DescribeImages(&aws_ec2.DescribeImagesInput{
		ImageIds: aws.StringSlice(images.Ids()),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe images: %s", err)
	}

	for _, awsImageInfo := range awsInfo.Images {
		for _, image := range images {
			if image.Id == *awsImageInfo.ImageId {
				image.Size = uint64(*awsImageInfo.BlockDeviceMappings[0].Ebs.VolumeSize * 1024 * 1024 * 1024)
				image.Type = models.IMAGE_FMT_AWS
				image.Arch = models.ParseHWArch(*awsImageInfo.Architecture)

				dateCreated, err := time.Parse(time.RFC3339, *awsImageInfo.CreationDate)
				if err != nil {
					logrus.WithError(err).
						WithField("ami", image.Id).
						Warning("failed to parse creation date")
					continue
				}
				image.Date = dateCreated
			}
		}
	}
	return images, nil
}

func NewAWSImagerep(ec2 *aws_ec2.EC2, imagesConfig []cfg.AWSImageConfig) *AWSImagerep {
	return &AWSImagerep{ec2: ec2, conf: imagesConfig}
}

func (repo *AWSImagerep) List(needleImages *models.ImageList) error {
	images, err := repo.fetchImages()
	if err != nil {
		return err
	}
	for _, image := range images {
		*needleImages = append(*needleImages, image)
	}
	return nil
}

func (repo *AWSImagerep) Get(needle *models.Image) (bool, error) {
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
