// +build unit

package handlers_test

import (
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

type ImageHandlersTestSuite struct {
	suite.Suite
	testool.WebTest
}

func (suite *ImageHandlersTestSuite) TestImageList_Ok() {
	suite.Authenticate()
	suite.Context.Hypervisors.Add(&dal.Hypervisor{
		Name: "test1",
		Images: &dal.StubImagerep{Data: []*models.Image{
			{
				FullName:   "test_image.img",
				OS:         "TestOS",
				Arch:       models.IMAGE_ARCH_X86,
				Type:       models.IMAGE_FMT_RAW,
				Date:       time.Unix(1484891107, 0),
				Hypervisor: "test1",
			},
			{
				FullName:   "test_image2.img",
				OS:         "OsTest-4.0",
				Arch:       models.IMAGE_ARCH_X86_64,
				Type:       models.IMAGE_FMT_QCOW2,
				Date:       time.Unix(1484831107, 0),
				Hypervisor: "test1",
			},
		}},
	})
	suite.Context.Hypervisors.Add(&dal.Hypervisor{
		Name: "test2",
		Images: &dal.StubImagerep{Data: []*models.Image{
			{
				FullName:   "test_image.img",
				OS:         "TestOS",
				Arch:       models.IMAGE_ARCH_X86,
				Type:       models.IMAGE_FMT_RAW,
				Date:       time.Unix(1484891107, 0),
				Hypervisor: "test2",
			},
			{
				FullName:   "test_image2.img",
				OS:         "OsTest-4.0",
				Arch:       models.IMAGE_ARCH_X86_64,
				Type:       models.IMAGE_FMT_QCOW2,
				Date:       time.Unix(1484831107, 0),
				Hypervisor: "test2",
			},
		}},
	})

	rr := suite.DoGet("/images/")
	suite.Assert().Equal(200, rr.Code, rr.Body.String())
}

func (suite *ImageHandlersTestSuite) TestImageList_RepFail() {
	suite.Authenticate()
	suite.Context.Hypervisors.Add(&dal.Hypervisor{
		Name: "test1",
		Images: &dal.StubImagerep{
			ListErr: fmt.Errorf("test repo error"),
		},
	})
	rr := suite.DoGet("/images/")
	suite.Assert().Equal(500, rr.Code, rr.Body.String())
}

func (suite *ImageHandlersTestSuite) TestImageList_AuthRequired() {
	rr := suite.DoGet("/images/")
	suite.Assert().Equal(302, rr.Code, rr.Body.String())
	suite.Assert().Equal(rr.Header().Get("Location"), "/login/?next=/images/")
}

func TestImageHandlersTestSuite(t *testing.T) {
	suite.Run(t, new(ImageHandlersTestSuite))
}
