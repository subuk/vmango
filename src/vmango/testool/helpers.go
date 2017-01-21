package testool

import (
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/sessions"
	"io"
	"net/http"
	"net/http/httptest"
	"vmango/web"
	web_router "vmango/web/router"
)

type StubSessionStore struct {
	Session *sessions.Session
}

func (s *StubSessionStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return s.Session, nil
}
func (s *StubSessionStore) New(r *http.Request, name string) (*sessions.Session, error) {
	return s.Session, nil
}
func (s *StubSessionStore) Save(r *http.Request, w http.ResponseWriter, sess *sessions.Session) error {
	return nil
}

func NewTestContext() *web.Context {
	ctx := &web.Context{}
	ctx.Router = web_router.New(ctx)
	ctx.Render = web.NewRenderer("", ctx)
	ctx.Logger = logrus.New()
	session := &sessions.Session{}
	session.Values = map[interface{}]interface{}{}
	ctx.SessionStore = &StubSessionStore{session}
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
