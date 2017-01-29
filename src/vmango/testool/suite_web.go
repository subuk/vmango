package testool

import (
	"github.com/gorilla/sessions"
	"io"
	"net/http"
	"net/http/httptest"
	"vmango/web"
)

type WebTest struct {
	Context *web.Context
}

func (suite *WebTest) SetupTest() {
	suite.Context = NewTestContext()
}

func (suite *WebTest) Authenticate() {
	suite.Session().Values["authuser"] = "testuser"
}

func (suite *WebTest) Session() *sessions.Session {
	return suite.Context.SessionStore.(*StubSessionStore).Session
}

func (suite *WebTest) DoRequest(method, url string, body io.Reader) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		panic(err)
	}
	if body != nil {
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	}
	suite.Context.Router.ServeHTTP(rr, req)
	return rr
}

func (suite *WebTest) DoGet(url string) *httptest.ResponseRecorder {
	return suite.DoRequest("GET", url, nil)
}

func (suite *WebTest) DoPost(url string, body io.Reader) *httptest.ResponseRecorder {
	return suite.DoRequest("POST", url, body)
}
