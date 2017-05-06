// +build integration

package dal_test

import (
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

type ImagerepLibvirtTestSuite struct {
	suite.Suite
	testool.LibvirtTest
	Imagerep *dal.LibvirtImagerep
}

func (suite *ImagerepLibvirtTestSuite) SetupSuite() {
	suite.LibvirtTest.SetupSuite()
	suite.LibvirtTest.Fixtures.Pools = []testool.LibvirtTestPoolFixture{
		{
			Name: "vmango-images-test",
			Volumes: []string{
				"Centos-7_amd64_qcow2.img",
				"Ubuntu-14.04_i386_qcow2.img",
				"Ubuntu-16.04_amd64_raw.img",
				"asdasdf",
				"Ubuntu-16.04_2323_raw.img",
				"Ubuntu-14.04_i386_222.img",
			},
		},
	}
}

func (suite *ImagerepLibvirtTestSuite) SetupTest() {
	suite.LibvirtTest.SetupTest()
	suite.Imagerep = dal.NewLibvirtImagerep(
		suite.LibvirtTest.VirConnect,
		suite.Fixtures.Pools[0].Name,
	)
}

func (suite *ImagerepLibvirtTestSuite) TestListOk() {
	images := models.ImageList{}
	err := suite.Imagerep.List(&images)
	suite.Require().NoError(err)

	suite.Equal(3, len(images))

	suite.Equal("Centos-7_amd64_qcow2.img", images[0].Id)
	suite.Equal("Centos-7", images[0].OS)
	suite.Equal("x86_64", images[0].Arch.String())
	suite.Equal(uint64(0x100000), images[0].Size)
	suite.Equal(models.IMAGE_FMT_QCOW2, images[0].Type)
	suite.True(images[0].Date.After(time.Time{}), images[0].Date.String())
	suite.Equal("vmango-images-test", images[0].PoolName)

	suite.Equal("Ubuntu-14.04_i386_qcow2.img", images[1].Id)
	suite.Equal("Ubuntu-14.04", images[1].OS)
	suite.Equal("x86", images[1].Arch.String())
	suite.Equal(uint64(0x100000), images[1].Size)
	suite.Equal(models.IMAGE_FMT_QCOW2, images[1].Type)
	suite.True(images[1].Date.After(time.Time{}), images[1].Date.String())
	suite.Equal("vmango-images-test", images[1].PoolName)

	suite.Equal("Ubuntu-16.04_amd64_raw.img", images[2].Id)
	suite.Equal("Ubuntu-16.04", images[2].OS)
	suite.Equal("x86_64", images[2].Arch.String())
	suite.Equal(uint64(0x100000), images[2].Size)
	suite.Equal(models.IMAGE_FMT_RAW, images[2].Type)
	suite.True(images[2].Date.After(time.Time{}), images[2].Date.String())
	suite.Equal("vmango-images-test", images[2].PoolName)
}

func (suite *ImagerepLibvirtTestSuite) TestGetOk() {
	image := &models.Image{Id: "Centos-7_amd64_qcow2.img"}
	exist, err := suite.Imagerep.Get(image)
	suite.Require().True(exist)
	suite.Require().NoError(err)

	suite.Equal("Centos-7_amd64_qcow2.img", image.Id)
	suite.Equal("Centos-7", image.OS)
	suite.Equal("x86_64", image.Arch.String())
	suite.Equal(uint64(0x100000), image.Size)
	suite.Equal(models.IMAGE_FMT_QCOW2, image.Type)
	suite.True(image.Date.After(time.Time{}), image.Date.String())
	suite.Equal("vmango-images-test", image.PoolName)
}

func (suite *ImagerepLibvirtTestSuite) TestGetNoIdFail() {
	image := &models.Image{}
	exist, err := suite.Imagerep.Get(image)
	suite.False(exist)
	suite.EqualError(err, "no image id provided")
}

func (suite *ImagerepLibvirtTestSuite) TestGetNoSuchImageFail() {
	image := &models.Image{Id: "doesntexist"}
	exist, err := suite.Imagerep.Get(image)
	suite.False(exist)
	suite.NoError(err)
}

func (suite *ImagerepLibvirtTestSuite) TestGetBadFilenameFail() {
	image := &models.Image{Id: "asdasdf"}
	exist, err := suite.Imagerep.Get(image)
	suite.True(exist)
	suite.EqualError(err, "invalid image: invalid name: asdasdf")
}

func TestImagerepLibvirtTestSuite(t *testing.T) {
	suite.Run(t, new(ImagerepLibvirtTestSuite))
}
