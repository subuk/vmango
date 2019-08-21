package testool

import (
	"io"
	"net/http"
	"net/http/httptest"
	"vmango/dal"
	"vmango/domain"
	"vmango/web"

	"github.com/gorilla/sessions"
)

type WebTest struct {
	Context *web.Context
	Headers http.Header

	SSHKeys         *dal.ConfigSSHKeyrep
	Plans           *dal.ConfigPlanrep
	ProviderFactory *dal.StubProviderFactory
}

func (suite *WebTest) SetupTest() {
	suite.Context = NewTestContext()
	suite.ProviderFactory = dal.NewStubProviderFactory()
	suite.Plans = &dal.ConfigPlanrep{}
	suite.SSHKeys = &dal.ConfigSSHKeyrep{}
	suite.Context.Machines = domain.NewMachineService(
		suite.ProviderFactory.Configs, suite.ProviderFactory.Produce,
		suite.SSHKeys, suite.Plans,
	)
	suite.Headers = http.Header{}
}

func (suite *WebTest) APIAuthenticate(username, password string) *WebTest {
	suite.Headers.Add("X-Vmango-User", username)
	suite.Headers.Add("X-Vmango-Pass", password)
	return suite
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
	req.Header = suite.Headers
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

func (suite *WebTest) DoDelete(url string) *httptest.ResponseRecorder {
	return suite.DoRequest("DELETE", url, nil)
}

func (suite *WebTest) DoBad(url string) *httptest.ResponseRecorder {
	return suite.DoRequest("BAD", url, nil)
}
