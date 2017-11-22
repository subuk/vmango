// +build integration

package dal_test

import (
	"strings"
	"testing"
	"vmango/dal"
	"vmango/domain"
	"vmango/testool"

	"github.com/stretchr/testify/suite"
)

type StatusrepLibvirtTestSuite struct {
	suite.Suite
	testool.LibvirtTest
	Repo *dal.LibvirtStatusrep
}

func (suite *StatusrepLibvirtTestSuite) SetupSuite() {
	suite.LibvirtTest.SetupSuite()
	suite.LibvirtTest.Fixtures.Domains = []string{"one", "two"}
	suite.LibvirtTest.Fixtures.Networks = []string{"vmango-test"}
	suite.LibvirtTest.Fixtures.Pools = []testool.LibvirtTestPoolFixture{
		{
			Name:    "vmango-vms-test",
			Volumes: []string{"one_disk", "one_config.iso", "two_disk", "two_config.iso"},
		},
		{
			Name:    "vmango-images-test",
			Volumes: []string{},
		},
	}
}

func (suite *StatusrepLibvirtTestSuite) SetupTest() {
	suite.LibvirtTest.SetupTest()
	suite.Repo = dal.NewLibvirtStatusrep(
		suite.LibvirtTest.VirConnect,
		suite.Fixtures.Pools[0].Name,
	)
}

func (suite *StatusrepLibvirtTestSuite) TestStatusOk() {
	status := &domain.StatusInfo{}
	err := suite.Repo.Fetch(status)
	suite.Require().NoError(err)

	suite.Equal("", status.Name)
	suite.Equal("libvirt", status.Type)
	suite.True(strings.HasPrefix(status.Description, "KVM hypervisor"), status.Description)
	suite.Equal(suite.LibvirtTest.VirURI, status.Connection)
	suite.Equal(2, status.MachineCount)
	suite.NotEqual(0, status.Memory.Total)
	suite.NotEqual(0, status.Storage.Total)
	// TODO: How to check it?
	// suite.NotEqual(0, status.Memory.Usage)
	// suite.NotEqual(0, status.Storage.Usage)

}

func TestStatusrepLibvirtTestSuite(t *testing.T) {
	suite.Run(t, new(StatusrepLibvirtTestSuite))
}
