// +build unit

package handlers_test

import (
	"bytes"
	"fmt"
	"testing"
	"vmango/dal"
	"vmango/domain"
	"vmango/testool"

	"github.com/stretchr/testify/suite"
)

func DELETE_URL(hypervisor, name string) string {
	return fmt.Sprintf("/machines/%s/%s/delete/", hypervisor, name)
}

func DELETE_API_URL(hypervisor, name string) string {
	return fmt.Sprintf("/api/machines/%s/%s/", hypervisor, name)
}

type MachineDeleteHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
	Repo *dal.StubMachinerep
}

func (suite *MachineDeleteHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	suite.Repo = &dal.StubMachinerep{}
	suite.ProviderFactory.Add(&domain.Provider{
		Name:     "testhv",
		Machines: suite.Repo,
	})
}

func (suite *MachineDeleteHandlerTestSuite) TestGetAuthRequired() {
	rr := suite.DoGet(DELETE_URL("testhv", "hello"))
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+DELETE_URL("testhv", "hello"))
}

func (suite *MachineDeleteHandlerTestSuite) TestPostAuthRequired() {
	rr := suite.DoPost(DELETE_URL("testhv", "hello"), nil)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+DELETE_URL("testhv", "hello"))
}

func (suite *MachineDeleteHandlerTestSuite) TestBadHTTPMethodNotAllowed() {
	suite.Authenticate()
	rr := suite.DoBad(DELETE_URL("testhv", "hello"))
	suite.Equal(501, rr.Code)
}

func (suite *MachineDeleteHandlerTestSuite) TestGetAPIAuthRequired() {
	rr := suite.DoGet(DELETE_API_URL("testhv", "hello"))
	suite.Equal(401, rr.Code, rr.Body.String())
	suite.Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.JSONEq(`{"Error": "Authentication failed"}`, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestPostAPINotImplemented() {
	suite.APIAuthenticate("admin", "secret")
	rr := suite.DoPost(DELETE_API_URL("testhv", "hello"), nil)
	suite.Equal(501, rr.Code, rr.Body.String())
	suite.Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.JSONEq(`{"Error": "Not implemented"}`, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestDeleteAPIAuthRequired() {
	rr := suite.DoDelete(DELETE_API_URL("testhv", "hello"))
	suite.Equal(401, rr.Code, rr.Body.String())
	suite.Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.JSONEq(`{"Error": "Authentication failed"}`, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestBadHTTPMethodAPINotAllowed() {
	suite.APIAuthenticate("admin", "secret")
	rr := suite.DoBad(DELETE_API_URL("testhv", "hello"))
	suite.Equal(501, rr.Code, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestConfirmationOk() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &domain.VirtualMachine{
		Id:       "deadbeefdeadbeefdeadbeefdeadbeef",
		Name:     "test-remove",
		RootDisk: &domain.VirtualMachineDisk{},
	}
	rr := suite.DoGet(DELETE_URL("testhv", "deadbeefdeadbeefdeadbeefdeadbeef"))
	suite.Equal(200, rr.Code, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestConfirmationNoMachineFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = false
	rr := suite.DoGet(DELETE_URL("testhv", "test-remove"))
	suite.Equal(500, rr.Code)
	suite.Contains(rr.Body.String(), "failed to fetch machine test-remove of provider testhv: not found")
}

func (suite *MachineDeleteHandlerTestSuite) TestConfirmationRepFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Error = fmt.Errorf("test error")
	rr := suite.DoGet(DELETE_URL("testhv", "test"))
	suite.Equal(500, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "failed to fetch machine test of provider testhv: test error")
}

func (suite *MachineDeleteHandlerTestSuite) TestActionOk() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &domain.VirtualMachine{
		Id:       "deadbeefdeadbeefdeadbeefdeadbeef",
		Name:     "test-remove",
		RootDisk: &domain.VirtualMachineDisk{},
	}
	rr := suite.DoPost(DELETE_URL("testhv", "deadbeefdeadbeefdeadbeefdeadbeef"), bytes.NewBuffer([]byte(``)))
	suite.Equal(302, rr.Code, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestAPIActionOk() {
	suite.APIAuthenticate("admin", "secret")
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &domain.VirtualMachine{
		Id:       "deadbeefdeadbeefdeadbeefdeadbeef",
		Name:     "test-remove",
		RootDisk: &domain.VirtualMachineDisk{},
	}
	rr := suite.DoDelete(DELETE_API_URL("testhv", "deadbeefdeadbeefdeadbeefdeadbeef"))
	suite.Equal(204, rr.Code, rr.Body.String())
	suite.Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.JSONEq(`{"Message": "Machine deadbeefdeadbeefdeadbeefdeadbeef of provider testhv deleted"}`, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestActionRepFail() {
	suite.Authenticate()
	suite.Repo.RemoveResponse = fmt.Errorf("test error")
	rr := suite.DoPost(DELETE_URL("testhv", "deadbeefdeadbeefdeadbeefdeadbeef"), bytes.NewBuffer([]byte(``)))
	suite.Equal(500, rr.Code)
}

func TestMacDeletemoveHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(MachineDeleteHandlerTestSuite))
}
