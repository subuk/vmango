package testool

import (
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
	store := suite.Context.SessionStore.(*StubSessionStore)
	store.Session.Values["authuser"] = "testuser"
}

func (suite *WebTest) DoRequest(method, url string, body io.Reader) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		panic(err)
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
