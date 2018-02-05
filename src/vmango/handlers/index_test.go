// +build unit

package handlers_test

import (
	"fmt"
	"testing"
	"vmango/dal"
	"vmango/domain"
	"vmango/testool"

	"github.com/stretchr/testify/suite"
)

const INDEX_URL = "/"

type IndexHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
}

func (suite *IndexHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	firstStatusrep := &dal.StubStatusrep{}
	firstStatusrep.FetchResponse.Status = &domain.StatusInfo{
		Name:        "test1",
		Type:        "stub",
		Description: "test1 description",
		Connection:  "conninfo",
	}
	firstStatusrep.FetchResponse.Status.Memory.Total = 1010101010
	firstStatusrep.FetchResponse.Status.Memory.Usage = 23
	firstStatusrep.FetchResponse.Status.Storage.Total = 2020202020
	firstStatusrep.FetchResponse.Status.Storage.Usage = 11
	firstMachinerep := &dal.StubMachinerep{}
	firstMachinerep.ListResponse.Machines = &domain.VirtualMachineList{
		&domain.VirtualMachine{},
		&domain.VirtualMachine{},
		&domain.VirtualMachine{},
	}
	firstProv := &domain.Provider{
		Name:     "test1",
		Status:   firstStatusrep,
		Machines: firstMachinerep,
	}

	secondStatusrep := &dal.StubStatusrep{}
	secondStatusrep.FetchResponse.Status = &domain.StatusInfo{
		Name:        "test2",
		Type:        "stub2",
		Description: "test2 description",
		Connection:  "conninfo2",
	}
	secondStatusrep.FetchResponse.Status.Memory.Total = 1010101
	secondStatusrep.FetchResponse.Status.Memory.Usage = 44
	secondStatusrep.FetchResponse.Status.Storage.Total = 2020202
	secondStatusrep.FetchResponse.Status.Storage.Usage = 36
	secondMachinerep := &dal.StubMachinerep{}
	secondMachinerep.ListResponse.Machines = &domain.VirtualMachineList{}
	secondProv := &domain.Provider{
		Name:     "test2",
		Status:   secondStatusrep,
		Machines: secondMachinerep,
	}
	suite.ProviderFactory.Add(firstProv)
	suite.ProviderFactory.Add(secondProv)
}

func (suite *IndexHandlerTestSuite) TestGetAuthRequired() {
	rr := suite.DoGet(INDEX_URL)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+INDEX_URL)
}

func (suite *IndexHandlerTestSuite) TestHTMLOk() {
	suite.Authenticate()
	rr := suite.DoGet(INDEX_URL)
	suite.Equal(200, rr.Code)
}

func (suite *IndexHandlerTestSuite) TestProviderFail() {
	failStatusrep := &dal.StubStatusrep{}
	failStatusrep.FetchResponse.Error = fmt.Errorf("test error")
	// machinerep := &dal.StubMachinerep{}
	failProv := &domain.Provider{
		Name:   "failprov",
		Status: failStatusrep,
	}
	suite.ProviderFactory.Add(failProv)
	suite.Authenticate()
	rr := suite.DoGet(INDEX_URL)
	suite.Equal(500, rr.Code)
	suite.Contains(rr.Body.String(), "failed to query provider &#39;failprov&#39; for status: test error")
}

func TestIndexHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(IndexHandlerTestSuite))
}
