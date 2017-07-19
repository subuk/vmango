// +build integration

package dal_test

import (
	"strings"
	"testing"
	"vmango/cfg"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"

	"github.com/stretchr/testify/suite"
)

type ProviderLibvirtSuite struct {
	suite.Suite
	testool.LibvirtTest
	Provider *dal.LibvirtProvider
}

func (suite *ProviderLibvirtSuite) SetupSuite() {
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

func (suite *ProviderLibvirtSuite) SetupTest() {
	suite.LibvirtTest.SetupTest()
	provider, err := dal.NewLibvirtProvider(cfg.HypervisorConfig{
		Name:             "testhv",
		Url:              suite.LibvirtTest.VirURI,
		RootStoragePool:  suite.Fixtures.Pools[0].Name,
		ImageStoragePool: suite.Fixtures.Pools[1].Name,
		Network:          suite.Fixtures.Networks[0],
		VmTemplate:       suite.VMTplPath,
		VolTemplate:      suite.VolTplPath,
		IgnoreVms:        []string{},
	})
	if err != nil {
		panic(err)
	}
	suite.Provider = provider
}

func (suite *ProviderLibvirtSuite) TestStatusOk() {
	status := &models.StatusInfo{}
	err := suite.Provider.Status(status)
	suite.Require().NoError(err)

	suite.Equal("testhv", status.Name)
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

func TestProviderLibvirtSuite(t *testing.T) {
	suite.Run(t, new(ProviderLibvirtSuite))
}
