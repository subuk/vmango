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

func DETAIL_URL(hypervisor, name string) string {
	return fmt.Sprintf("/machines/%s/%s/", hypervisor, name)
}

func DETAIL_API_URL(hypervisor, name string) string {
	return fmt.Sprintf("/api/machines/%s/%s/", hypervisor, name)
}

type MachineDetailHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
	Repo *dal.StubMachinerep
}

func (suite *MachineDetailHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	suite.Repo = &dal.StubMachinerep{}
	suite.Context.Hypervisors.Add(&dal.Hypervisor{
		Name:     "testhv",
		Machines: suite.Repo,
	})
}

func (suite *MachineDetailHandlerTestSuite) TestAuthRequired() {
	rr := suite.DoGet(DETAIL_URL("testhv", "hello"))
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+DETAIL_URL("testhv", "hello"))
}

func (suite *MachineDetailHandlerTestSuite) TestAPIAuthRequired() {
	rr := suite.DoGet(DETAIL_API_URL("testhv", "hello"))
	suite.Equal(401, rr.Code, rr.Body.String())
	suite.Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.JSONEq(`{"Error": "Authentication failed"}`, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestHTMLOk() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name:       "test-detail-html",
		Hypervisor: "testhv",
		Ip:         &models.IP{Address: "1.1.1.1"},
		RootDisk: &models.VirtualMachineDisk{
			Size:   123,
			Driver: "hello",
			Type:   "wow",
		},
	}
	rr := suite.DoGet(DETAIL_URL("testhv", "test-detail-html"))
	suite.Equal("text/html; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.Equal(200, rr.Code, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestAPIOk() {
	suite.APIAuthenticate("admin", "secret")
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name:       "test-detail-json",
		Uuid:       "123uuid",
		Hypervisor: "stub",
		Memory:     456,
		Cpus:       1,
		HWAddr:     "hw:hw:hw",
		VNCAddr:    "vnc",
		OS:         "HelloOS",
		Arch:       "xxx",
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
	rr := suite.DoGet(DETAIL_API_URL("testhv", "test-detail-json"))
	suite.Require().Equal(200, rr.Code, rr.Body.String())
	suite.Require().Equal("application/json; charset=UTF-8", rr.Header().Get("Content-Type"))
	expected := `{
      "Title": "Machine test-detail-json",
      "Machine": {
          "Name": "test-detail-json",
          "Memory": 456,
          "Cpus": 1,
          "Ip": {"Address": "1.1.1.1", "Gateway": "", "Netmask": 0, "UsedBy": ""},
          "HWAddr": "hw:hw:hw",
          "Hypervisor": "stub",
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

func (suite *MachineDetailHandlerTestSuite) TestPostNotAllowed() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name:       "hello",
		Uuid:       "123uuid",
		Hypervisor: "stub",
		Memory:     456,
		Cpus:       1,
		HWAddr:     "hw:hw:hw",
		VNCAddr:    "vnc",
		OS:         "HelloOS",
		Arch:       "xxx",
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
	rr := suite.DoPost(DETAIL_URL("testhv", "hello"), nil)
	suite.Equal(501, rr.Code, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestPostAPINotAllowed() {
	suite.APIAuthenticate("admin", "secret")
	suite.Repo.GetResponse.Exist = true
	suite.Repo.GetResponse.Machine = &models.VirtualMachine{
		Name:       "hello",
		Uuid:       "123uuid",
		Hypervisor: "stub",
		Memory:     456,
		Cpus:       1,
		HWAddr:     "hw:hw:hw",
		VNCAddr:    "vnc",
		OS:         "HelloOS",
		Arch:       "xxx",
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
	rr := suite.DoPost(DETAIL_API_URL("testhv", "hello"), nil)
	suite.Equal(501, rr.Code, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestRepFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Error = fmt.Errorf("test error")
	rr := suite.DoGet(DETAIL_URL("testhv", "hello"))
	suite.Equal(500, rr.Code, rr.Body.String())
}

func (suite *MachineDetailHandlerTestSuite) TestMachineNotFoundFail() {
	suite.Authenticate()
	suite.Repo.GetResponse.Exist = false
	rr := suite.DoGet(DETAIL_URL("testhv", "doesntexist"))
	suite.Equal(404, rr.Code, rr.Body.String())
}

func TestMachineDetailHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(MachineDetailHandlerTestSuite))
}
