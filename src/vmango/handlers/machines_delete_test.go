// +build unit

package handlers_test

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

func DELETE_URL(name string) string {
	return fmt.Sprintf("/machines/%s/delete/", name)
}

type MachineDeleteHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
	Repo   *dal.StubMachinerep
	IPPool *dal.StubIPPool
}

func (suite *MachineDeleteHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	suite.Repo = &dal.StubMachinerep{}
	suite.IPPool = &dal.StubIPPool{}
	suite.Context.Machines = suite.Repo
	suite.Context.IPPool = suite.IPPool
}

func (suite *MachineDeleteHandlerTestSuite) TestAuthRequired() {
	rr := suite.DoGet(DELETE_URL("hello"))
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+DELETE_URL("hello"))
}

func (suite *MachineDeleteHandlerTestSuite) TestConfirmationOk() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name: "test-remove",
		Disk: &models.VirtualMachineDisk{},
	}
	rr := suite.DoGet(DELETE_URL("test-remove"))
	suite.Equal(200, rr.Code, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestConfirmationNoMachineFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = false
	rr := suite.DoGet(DELETE_URL("test-remove"))
	suite.Equal(404, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "Machine with name test-remove not found")
}

func (suite *MachineDeleteHandlerTestSuite) TestConfirmationRepFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Error = fmt.Errorf("test error")
	rr := suite.DoGet(DELETE_URL("test"))
	suite.Equal(500, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "failed to fetch machine info: test error")
}

func (suite *MachineDeleteHandlerTestSuite) TestActionOk() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name: "test-remove",
		Disk: &models.VirtualMachineDisk{},
	}
	rr := suite.DoPost(DELETE_URL("test-remove"), bytes.NewBuffer([]byte(``)))
	suite.Equal(302, rr.Code, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestActionNoMachineFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = false
	rr := suite.DoPost(DELETE_URL("test-remove"), bytes.NewBuffer([]byte(``)))
	suite.Equal(404, rr.Code, rr.Body.String())
}

func (suite *MachineDeleteHandlerTestSuite) TestActionRepFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Error = fmt.Errorf("test error")
	rr := suite.DoPost(DELETE_URL("test-remove"), bytes.NewBuffer([]byte(``)))
	suite.Contains(rr.Body.String(), "failed to fetch machine info: test error")
	suite.Equal(500, rr.Code, rr.Body.String())
}

func TestMacDeletemoveHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(MachineDeleteHandlerTestSuite))
}