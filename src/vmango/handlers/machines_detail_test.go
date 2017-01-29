// +build unit

package handlers_test

import (
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

func DETAIL_URL(name string) string {
	return fmt.Sprintf("/machines/%s/", name)
}

type MachineDetailHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
	Repo *dal.StubMachinerep
}

func (suite *MachineDetailHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	suite.Repo = &dal.StubMachinerep{}
	suite.Context.Machines = suite.Repo
}

func (suite *MachineDetailHandlerTestSuite) TestAuthRequired() {
	rr := suite.DoGet(DETAIL_URL("hello"))
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+DETAIL_URL("hello"))
}

func (suite *MachineDetailHandlerTestSuite) TestHTMLOk() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name: "test-detail-html",
		RootDisk: &models.VirtualMachineDisk{
			Size:   123,
			Driver: "hello",
			Type:   "wow",
		},
	}
	rr := suite.DoGet(DETAIL_URL("test-detail-html"))
	suite.Equal("text/html; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.Equal(200, rr.Code, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestJSONOk() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name:    "test-detail-json",
		Uuid:    "123uuid",
		Memory:  456,
		Cpus:    1,
		HWAddr:  "hw:hw:hw",
		VNCAddr: "vnc",
		OS:      "HelloOS",
		Arch:    "xxx",
		Ip: &models.IP{
			Address: "1.1.1.1",
		},
		RootDisk: &models.VirtualMachineDisk{
			Size:   123,
			Driver: "hello",
			Type:   "wow",
		},
		SSHKeys: []*models.SSHKey{
			{Name: "test", Public: "keykeykey"},
		},
	}
	rr := suite.DoGet(DETAIL_URL("test-detail-json") + "?format=json")
	suite.Require().Equal(200, rr.Code, rr.Body.String())
	suite.Require().Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	expected := `{
      "Machine": {
          "Name": "test-detail-json",
          "Memory": 456,
          "Cpus": 1,
          "Ip": {"Address": "1.1.1.1", "Gateway": "", "Netmask": 0, "UsedBy": ""},
          "HWAddr": "hw:hw:hw",
          "VNCAddr": "vnc",
          "OS": "HelloOS",
          "Arch": "xxx",
          "RootDisk": {
            "Size": 123,
            "Driver": "hello",
            "Type": "wow"
           },
          "SSHKeys": [
            {"Name": "test", "Public": "keykeykey"}
          ]
      }
  }`
	suite.JSONEq(expected, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestRepFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Error = fmt.Errorf("test error")
	rr := suite.DoGet(DETAIL_URL("hello"))
	suite.Equal(500, rr.Code, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestMachineNotFoundFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = false
	rr := suite.DoGet(DETAIL_URL("doesntexist"))
	suite.Equal(404, rr.Code, rr.Body.String())
}

func TestMachineDetailHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(MachineDetailHandlerTestSuite))
}
