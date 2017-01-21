package handlers_test

import (
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
	"vmango/dal"
	"vmango/handlers"
	"vmango/models"
	"vmango/testool"
)

type ImageHandlersTestSuite struct {
	suite.Suite
	testool.WebTest
}

func (suite *ImageHandlersTestSuite) TestImageList_Ok() {
	suite.Authenticate()
	suite.Context.Images = &dal.StubImagerep{Data: []*models.Image{
		{
			FullName: "test_image.img",
			OS:       "TestOS",
			Arch:     models.IMAGE_ARCH_X86,
			Type:     models.IMAGE_FMT_RAW,
			Date:     time.Unix(1484891107, 0),
		},
		{
			FullName: "test_image2.img",
			OS:       "OsTest-4.0",
			Arch:     models.IMAGE_ARCH_X86_64,
			Type:     models.IMAGE_FMT_QCOW2,
			Date:     time.Unix(1484831107, 0),
		},
	}}
	rr := suite.DoGet(handlers.ImageList)
	suite.Assert().Equal(200, rr.Code, rr.Body.String())
}

func (suite *ImageHandlersTestSuite) TestImageList_RepFail() {
	suite.Authenticate()
	suite.Context.Images = &dal.StubImagerep{
		ListErr: fmt.Errorf("test repo error"),
	}
	rr := suite.DoGet(handlers.ImageList)
	suite.Assert().Equal(500, rr.Code, rr.Body.String())
}

func (suite *ImageHandlersTestSuite) TestImageList_AuthRequired() {
	rr := suite.DoGet(handlers.ImageList, "/redirect")
	suite.Assert().Equal(302, rr.Code, rr.Body.String())
	suite.Assert().Equal(rr.Header().Get("Location"), "/login/?next=/redirect")
}

func TestImageHandlersTestSuite(t *testing.T) {
	suite.Run(t, new(ImageHandlersTestSuite))
}
