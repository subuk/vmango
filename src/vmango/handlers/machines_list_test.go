package handlers_test

import (
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

const LIST_URL = "/machines/"

type MachineListHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
	Repo *dal.StubMachinerep
}

func (suite *MachineListHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	suite.Repo = &dal.StubMachinerep{}
	suite.Context.Machines = suite.Repo
}

func (suite *MachineListHandlerTestSuite) TestAuthRequired() {
	rr := suite.DoGet(LIST_URL)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+LIST_URL)
}

func (suite *MachineListHandlerTestSuite) TestHTMLOk() {
	suite.Authenticate()
	suite.Repo.ListResponse.Machines = &models.VirtualMachineList{}
	suite.Repo.ListResponse.Machines.Add(&models.VirtualMachine{Name: "test"})
	rr := suite.DoGet(LIST_URL)
	suite.Equal(200, rr.Code, rr.Body.String())
	suite.Equal("text/html; charset=UTF-8", rr.Header().Get("Content-Type"))
}

func (suite *MachineListHandlerTestSuite) TestJSONOk() {
	suite.Authenticate()
	suite.Repo.ListResponse.Machines = &models.VirtualMachineList{}
	suite.Repo.ListResponse.Machines.Add(&models.VirtualMachine{
		Name:    "test",
		Uuid:    "123uuid",
		Memory:  456,
		Cpus:    1,
		HWAddr:  "hw:hw:hw",
		VNCAddr: "vnc",
		Ip: &models.IP{
			Address: "1.1.1.1",
		},
		Disk: &models.VirtualMachineDisk{
			Size:   123,
			Driver: "hello",
			Type:   "wow",
		},
		SSHKeys: []*models.SSHKey{
			{Name: "test", Public: "keykeykey"},
		},
	})
	suite.Repo.ListResponse.Machines.Add(&models.VirtualMachine{
		Name:    "hello",
		Uuid:    "321uuid",
		Memory:  67897,
		Cpus:    4,
		HWAddr:  "xx:xx:xx",
		VNCAddr: "VVV",
		Ip: &models.IP{
			Address: "2.2.2.2",
		},
		Disk: &models.VirtualMachineDisk{
			Size:   321,
			Driver: "ehlo",
			Type:   "www",
		},
		SSHKeys: []*models.SSHKey{
			{Name: "test2", Public: "kekkekkek"},
		},
	})

	rr := suite.DoGet(LIST_URL + "?format=json")
	suite.Require().Equal(200, rr.Code, rr.Body.String())
	suite.Require().Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	expected := `{
      "Machines": [{
          "Name": "test",
          "Memory": 456,
          "Cpus": 1,
          "Ip": {"Address": "1.1.1.1", "Gateway": "", "Netmask": 0, "UsedBy": ""},
          "HWAddr": "hw:hw:hw",
          "VNCAddr": "vnc",
          "Disk": {
            "Size": 123,
            "Driver": "hello",
            "Type": "wow"
           },
          "SSHKeys": [
            {"Name": "test", "Public": "keykeykey"}
          ]
        }, {
          "Name": "hello",
          "Memory": 67897,
          "Cpus": 4,
          "HWAddr": "xx:xx:xx",
          "VNCAddr": "VVV",
          "Ip": {"Address": "2.2.2.2", "Gateway": "", "Netmask": 0, "UsedBy": ""},
          "Disk": {
            "Size": 321,
            "Driver": "ehlo",
            "Type": "www"
           },
          "SSHKeys": [
            {"Name": "test2", "Public": "kekkekkek"}
          ]
        }]
    }`
	suite.JSONEq(expected, rr.Body.String())
}

func (suite *MachineListHandlerTestSuite) TestRepFail() {
	suite.Authenticate()
	suite.Repo.ListResponse.Error = fmt.Errorf("test error")
	rr := suite.DoGet(LIST_URL)
	suite.Equal(500, rr.Code, rr.Body.String())
}

func TestMachineListHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(MachineListHandlerTestSuite))
}
