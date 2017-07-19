// +build unit

package handlers_test

import (
	"fmt"
	"testing"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"

	"github.com/stretchr/testify/suite"
)

const INDEX_URL = "/"

type IndexHandlerTestSuite struct {
	suite.Suite
	testool.WebTest
	Providers *dal.Providers
}

func (suite *IndexHandlerTestSuite) SetupTest() {
	suite.WebTest.SetupTest()
	firstProv := &dal.StubProvider{
		TName: "test1",
	}
	firstProv.StatusResponse.Status = &models.StatusInfo{
		Name:         "test1",
		Type:         "stub",
		Description:  "test1 description",
		Connection:   "conninfo",
		MachineCount: 3,
	}
	firstProv.StatusResponse.Status.Memory.Total = 1010101010
	firstProv.StatusResponse.Status.Memory.Usage = 23
	firstProv.StatusResponse.Status.Storage.Total = 2020202020
	firstProv.StatusResponse.Status.Storage.Usage = 11

	secondProv := &dal.StubProvider{
		TName: "test2",
	}
	secondProv.StatusResponse.Status = &models.StatusInfo{
		Name:         "test2",
		Type:         "stub2",
		Description:  "test2 description",
		Connection:   "conninfo2",
		MachineCount: 0,
	}
	secondProv.StatusResponse.Status.Memory.Total = 1010101
	secondProv.StatusResponse.Status.Memory.Usage = 44
	secondProv.StatusResponse.Status.Storage.Total = 2020202
	secondProv.StatusResponse.Status.Storage.Usage = 36
	suite.Context.Providers.Add(firstProv)
	suite.Context.Providers.Add(secondProv)

}

func (suite *IndexHandlerTestSuite) TestGetAuthRequired() {
	rr := suite.DoGet(INDEX_URL)
	suite.Equal(302, rr.Code, rr.Body.String())
	suite.Equal(rr.Header().Get("Location"), "/login/?next="+INDEX_URL)
}

func (suite *IndexHandlerTestSuite) TestHTMLOk() {
	suite.Authenticate()
	rr := suite.DoGet(INDEX_URL)
	suite.Equal(200, rr.Code, rr.Body.String())
}

func (suite *IndexHandlerTestSuite) TestProviderFail() {
	failProv := &dal.StubProvider{
		TName: "failprov",
	}
	failProv.StatusResponse.Err = fmt.Errorf("test error")
	suite.Context.Providers.Add(failProv)

	suite.Authenticate()
	rr := suite.DoGet(INDEX_URL)
	suite.Equal(500, rr.Code)
	suite.Contains(rr.Body.String(), "failed to query provider failprov for status: test error")
}

func TestIndexHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(IndexHandlerTestSuite))
}
