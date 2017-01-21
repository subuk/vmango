package testool

import (
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

func (suite *WebTest) DoGet(handler web.HandlerFunc, url ...string) *httptest.ResponseRecorder {
	testUrl := ""
	if len(url) > 1 {
		panic("too many urls")
	}
	if len(url) == 1 {
		testUrl = url[0]
	}
	httphandler := web.NewHandler(suite.Context, handler)
	return DoGet(httphandler, testUrl)
}
