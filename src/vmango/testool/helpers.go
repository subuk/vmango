package testool

import (
	"github.com/Sirupsen/logrus"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"vmango/web"
	web_router "vmango/web/router"
)

func NewTestContext() *web.Context {
	_, filename, _, _ := runtime.Caller(0)
	sourceDir, err := filepath.Abs(
		filepath.Join(filepath.Dir(filename), "../../../"),
	)
	if err != nil {
		panic(err)
	}
	ctx := &web.Context{}
	ctx.Router = web_router.New(filepath.Join(sourceDir, "static"), ctx)
	ctx.Render = web.NewRenderer(filepath.Join(sourceDir, "templates"), ctx)
	ctx.Logger = logrus.New()
	return ctx
}

func DoRequest(handler *web.Handler, method, url string, body io.Reader) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		panic(err)
	}
	handler.ServeHTTP(rr, req)
	return rr

}

func DoGet(handler *web.Handler, url string) *httptest.ResponseRecorder {
	return DoRequest(handler, "GET", url, nil)
}
