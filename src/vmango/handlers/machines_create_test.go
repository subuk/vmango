// +build unit

package handlers_test

import (
	"bytes"
	"github.com/stretchr/testify/suite"
	"net/url"
	"testing"
	"vmango/cfg"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

const CREATE_URL = "/machines/add/"

type MachineCreateHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
	Machines *dal.StubMachinerep
}

func (suite *MachineCreateHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	suite.Machines = &dal.StubMachinerep{}
	suite.Context.Machines = suite.Machines
	suite.Context.Images = &dal.StubImagerep{
		Data: []*models.Image{
			{OS: "TestOS-1.0", Arch: models.IMAGE_ARCH_X86_64, Size: 10 * 1024 * 1024, Type: models.IMAGE_FMT_QCOW2, FullName: "TestOS-1.0_amd64.img", PoolName: "test"},
		},
	}
	suite.Context.SSHKeys = dal.NewConfigSSHKeyrep([]cfg.SSHKeyConfig{
		{Name: "first", Public: "hello"},
	})
	suite.Context.Plans = dal.NewConfigPlanrep([]cfg.PlanConfig{
		{Name: "test-1", Memory: 512 * 1024 * 1024, Cpus: 1, DiskSize: 5},
		{Name: "test-2", Memory: 1024 * 1024 * 1024, Cpus: 2, DiskSize: 10},
	})
}

func (suite *MachineCreateHandlerTestSuite) TestAuthRequired() {
	rr := suite.DoGet(CREATE_URL)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+CREATE_URL)
}

func (suite *MachineCreateHandlerTestSuite) TestGetOk() {
	suite.Authenticate()
	rr := suite.DoGet(CREATE_URL)
	suite.Equal("text/html; charset=UTF-8", rr.Header().Get("Content-Type"))
	suite.Equal(200, rr.Code, rr.Body.String())
}

func (suite *MachineCreateHandlerTestSuite) TestCreateOk() {
	suite.Authenticate()
	data := bytes.NewBufferString((url.Values{
		"Name":   []string{"testvm"},
		"Plan":   []string{"test-1"},
		"Image":  []string{"TestOS-1.0_amd64.img"},
		"SSHKey": []string{"first"},
	}).Encode())
	suite.T().Log(data)
	rr := suite.DoPost(CREATE_URL, data)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/machines/testvm/")
}

func (suite *MachineCreateHandlerTestSuite) TestCreateSameNameAlreadyExistFail() {
	suite.Authenticate()
	data := bytes.NewBufferString((url.Values{
		"Name":   []string{"exist"},
		"Plan":   []string{"test-1"},
		"Image":  []string{"TestOS-1.0_amd64.img"},
		"SSHKey": []string{"first"},
	}).Encode())
	suite.Machines.GetResponse.Exist = true
	suite.T().Log(data)
	rr := suite.DoPost(CREATE_URL, data)
	suite.Equal(400, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "machine with name &#39;exist&#39; already exists")
	suite.Equal(rr.Header().Get("Location"), "")
}

func (suite *MachineCreateHandlerTestSuite) TestCreateNoPlanFail() {
	suite.Authenticate()
	data := bytes.NewBufferString((url.Values{
		"Name":   []string{"testvm"},
		"Plan":   []string{"doesntexist"},
		"Image":  []string{"TestOS-1.0_amd64.img"},
		"SSHKey": []string{"first"},
	}).Encode())
	suite.T().Log(data)
	rr := suite.DoPost(CREATE_URL, data)
	suite.Equal(400, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "plan &#34;doesntexist&#34; not found")
	suite.Equal(rr.Header().Get("Location"), "")
}

func (suite *MachineCreateHandlerTestSuite) TestCreateNoImageFail() {
	suite.Authenticate()
	data := bytes.NewBufferString((url.Values{
		"Name":   []string{"testvm"},
		"Plan":   []string{"test-1"},
		"Image":  []string{"doesntexist"},
		"SSHKey": []string{"first"},
	}).Encode())
	suite.T().Log(data)
	rr := suite.DoPost(CREATE_URL, data)
	suite.Equal(400, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "image &#34;doesntexist&#34; not found")
	suite.Equal(rr.Header().Get("Location"), "")
}

func (suite *MachineCreateHandlerTestSuite) TestCreateNoSSHKeyFail() {
	suite.Authenticate()
	data := bytes.NewBufferString((url.Values{
		"Name":   []string{"testvm"},
		"Plan":   []string{"test-1"},
		"Image":  []string{"TestOS-1.0_amd64.img"},
		"SSHKey": []string{"doesntexist"},
	}).Encode())
	suite.T().Log(data)
	rr := suite.DoPost(CREATE_URL, data)
	suite.Equal(400, rr.Code, rr.Body.String())
	suite.Contains(rr.Body.String(), "ssh key &#39;doesntexist&#39; doesn&#39;t exist")
	suite.Equal(rr.Header().Get("Location"), "")
}

func TestMachineCreateHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(MachineCreateHandlerTestSuite))
}
