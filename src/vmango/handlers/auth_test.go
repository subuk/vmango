// +build unit

package handlers_test

import (
	"bytes"
	"github.com/stretchr/testify/suite"
	"net/url"
	"testing"
	"vmango/cfg"
	"vmango/dal"
	"vmango/testool"
)

const LOGIN_URL = "/login/"

type LoginHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
}

func (suite *LoginHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	suite.Context.AuthDB = dal.NewConfigAuthrep([]cfg.AuthUserConfig{
		// Password: secret
		{Username: "testadmin", PasswordHash: "$2a$10$wrob4Gq/7x.zcaZu6wwkYueSCp3KMYC8Z.X.TR.04mMMHt5dM6rCe"},
	})
}

func (suite *LoginHandlerTestSuite) TestGetOk() {
	rr := suite.DoGet(LOGIN_URL)
	suite.Equal("text/html; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.Equal(200, rr.Code, rr.Body.String())
}

func (suite *LoginHandlerTestSuite) TestLoginOk() {
	data := bytes.NewBufferString((url.Values{
		"Username": []string{"testadmin"},
		"Password": []string{"secret"},
	}).Encode())
	rr := suite.DoPost(LOGIN_URL+"?next=/machines/testvm/", data)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/machines/testvm/")
	suite.Equal("testadmin", suite.Session().Values["authuser"])
}

func (suite *LoginHandlerTestSuite) TestNextRespectedOk() {
	data := bytes.NewBufferString((url.Values{
		"Username": []string{"testadmin"},
		"Password": []string{"secret"},
	}).Encode())
	rr := suite.DoPost(LOGIN_URL, data)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/")
}

func (suite *LoginHandlerTestSuite) TestNoUserFail() {
	data := bytes.NewBufferString((url.Values{
		"Username": []string{"doesntexist"},
		"Password": []string{"xxx"},
	}).Encode())
	rr := suite.DoPost(LOGIN_URL, data)
	suite.Equal(400, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "authentication failed")
}

func (suite *LoginHandlerTestSuite) TestInvalidPasswordFail() {
	data := bytes.NewBufferString((url.Values{
		"Username": []string{"testadmin"},
		"Password": []string{"badpw"},
	}).Encode())
	rr := suite.DoPost(LOGIN_URL, data)
	suite.Equal(400, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "authentication failed")
}

func TestLoginHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(LoginHandlerTestSuite))
}
