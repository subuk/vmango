package handlers_test

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
	"vmango/dal"
	"vmango/handlers"
	"vmango/models"
	"vmango/testool"
	"vmango/web"
)

func TestImageList_Ok(t *testing.T) {
	ctx := testool.NewTestContext()
	ctx.Images = &dal.StubImagerep{Data: []*models.Image{
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
	handler := web.NewHandler(ctx, handlers.ImageList)
	rr := testool.DoGet(handler, "")
	assert.Equal(t, 200, rr.Code, rr.Body.String())
}

func TestImageList_RepFail(t *testing.T) {
	ctx := testool.NewTestContext()
	ctx.Images = &dal.StubImagerep{
		ListErr: fmt.Errorf("test repo error"),
	}
	handler := web.NewHandler(ctx, handlers.ImageList)
	rr := testool.DoGet(handler, "")
	assert.Equal(t, 500, rr.Code, rr.Body.String())
}
