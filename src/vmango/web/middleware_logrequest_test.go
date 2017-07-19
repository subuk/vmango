// +build unit

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Sirupsen/logrus"
	logrus_test "github.com/Sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/suite"
)

type LogRequestMiddlewareTestSuite struct {
	suite.Suite
	Logger     *logrus.Logger
	Hook       *logrus_test.Hook
	NextCalled bool
}

func (suite *LogRequestMiddlewareTestSuite) dummyNext(rw http.ResponseWriter, req *http.Request) {
	suite.NextCalled = true
}

func (suite *LogRequestMiddlewareTestSuite) call(req *http.Request, exclude, trusted []string) *httptest.ResponseRecorder {
	logger, hook := logrus_test.NewNullLogger()
	suite.Logger = logger
	suite.Hook = hook
	suite.NextCalled = false

	rr := httptest.NewRecorder()
	mw := &LogRequestMiddleware{
		logger:          suite.Logger,
		excludePrefixes: exclude,
		trustedProxies:  trusted,
	}
	mw.ServeHTTP(rr, req, suite.dummyNext)
	return rr
}

func (suite *LogRequestMiddlewareTestSuite) TestOk() {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.1.1.1:2322"
	suite.call(req, []string{}, []string{})
	suite.Len(suite.Hook.Entries, 1)
	entry := suite.Hook.Entries[0]
	suite.Equal("/test", entry.Data["path"])
	suite.NotEqual(0, entry.Data["latency"])
	suite.Equal("1.1.1.1", entry.Data["remote"])
	suite.True(suite.NextCalled)
}

func (suite *LogRequestMiddlewareTestSuite) TestExcludePrefixOk() {
	req := httptest.NewRequest("GET", "/static/hello.css", nil)
	suite.call(req, []string{"/static/"}, []string{})
	suite.Len(suite.Hook.Entries, 0)
	suite.True(suite.NextCalled)

	req = httptest.NewRequest("GET", "/test/hello.css", nil)
	suite.call(req, []string{"/static/"}, []string{})
	suite.Len(suite.Hook.Entries, 1)
	suite.True(suite.NextCalled)
}

func (suite *LogRequestMiddlewareTestSuite) TestTrustedRealIpHeaderOk() {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.1.1.1:2322"
	req.Header.Add("X-Real-Ip", "2.2.2.2")
	suite.call(req, []string{}, []string{"1.1.1.1"})
	suite.Len(suite.Hook.Entries, 1)
	entry := suite.Hook.Entries[0]
	suite.Equal("2.2.2.2", entry.Data["remote"])
}

func (suite *LogRequestMiddlewareTestSuite) TestNoColonInRemoteAddrOk() {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.1.1.1"
	suite.call(req, []string{}, []string{})
	suite.Len(suite.Hook.Entries, 1)
	entry := suite.Hook.Entries[0]
	suite.Equal("1.1.1.1", entry.Data["remote"])
}

func (suite *LogRequestMiddlewareTestSuite) TestNotTrustedRealIpHeaderOk() {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.1.1.1:2322"
	req.Header.Add("X-Real-Ip", "2.2.2.2")
	suite.call(req, []string{}, []string{})
	suite.Len(suite.Hook.Entries, 1)
	entry := suite.Hook.Entries[0]
	suite.Equal("1.1.1.1", entry.Data["remote"])
}

func (suite *LogRequestMiddlewareTestSuite) TestTrustedXForwardedForHeaderOk() {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.1.1.1:2322"
	req.Header.Add("X-Forwarded-For", "3.3.3.3, 4.4.4.4, 5.5.5.5")
	suite.call(req, []string{}, []string{"1.1.1.1"})
	suite.Len(suite.Hook.Entries, 1)
	entry := suite.Hook.Entries[0]
	suite.Equal("3.3.3.3", entry.Data["remote"])
}

func (suite *LogRequestMiddlewareTestSuite) TestNotTrustedXForwardedForHeaderOk() {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.1.1.1:2322"
	req.Header.Add("X-Forwarded-For", "3.3.3.3, 4.4.4.4, 5.5.5.5")
	suite.call(req, []string{}, []string{})
	suite.Len(suite.Hook.Entries, 1)
	entry := suite.Hook.Entries[0]
	suite.Equal("1.1.1.1", entry.Data["remote"])
}

func (suite *LogRequestMiddlewareTestSuite) TestTrustedXForwardedForHeaderTrimedOk() {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.1.1.1:2322"
	req.Header.Add("X-Forwarded-For", " 3.3.3.3  , 4.4.4.4")
	suite.call(req, []string{}, []string{"1.1.1.1"})
	suite.Len(suite.Hook.Entries, 1)
	entry := suite.Hook.Entries[0]
	suite.Equal("3.3.3.3", entry.Data["remote"])
}

func TestLogRequestMiddlewareTestSuite(t *testing.T) {
	suite.Run(t, new(LogRequestMiddlewareTestSuite))
}
