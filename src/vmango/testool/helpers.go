package testool

import (
	"encoding/xml"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/gorilla/sessions"
	"github.com/libvirt/libvirt-go"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"vmango/cfg"
	"vmango/dal"
	"vmango/web"
	web_router "vmango/web/router"
)

type StubSessionStore struct {
	Session *sessions.Session
}

func (s *StubSessionStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return s.Session, nil
}
func (s *StubSessionStore) New(r *http.Request, name string) (*sessions.Session, error) {
	return s.Session, nil
}
func (s *StubSessionStore) Save(r *http.Request, w http.ResponseWriter, sess *sessions.Session) error {
	return nil
}

func StubCsrfProtect() web_router.CSRFProtector {
	return func(handler http.Handler) http.Handler {
		return handler
	}
}

func NewTestContext() *web.Context {
	ctx := &web.Context{}
	ctx.Router = web_router.New(ctx, StubCsrfProtect())
	ctx.Render = web.NewRenderer("", true, ctx)
	ctx.AuthDB = dal.NewConfigAuthrep([]cfg.AuthUserConfig{
		{Username: "admin", PasswordHash: "$2a$10$K6XfNbM2e5Tn/etSW7HpvuCAsWT62Y1Zrcituk9U1ktAHHVYh5kBS"},
	})
	ctx.Logger = logrus.New()
	ctx.Providers = dal.Providers{}
	store := &StubSessionStore{}
	session := sessions.NewSession(store, "vmango")
	session.Values = map[interface{}]interface{}{}
	store.Session = session
	ctx.SessionStore = store
	return ctx
}

func SourceDir() string {
	_, filename, _, _ := runtime.Caller(1)
	absPath, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../"))
	if err != nil {
		panic(err)
	}
	return absPath
}

func CreateDisksForMachines(conn *libvirt.Connect, poolName string) error {
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		domainXML, err := domain.GetXMLDesc(0)
		if err != nil {
			panic(err)
		}
		info := struct {
			Disk []struct {
				Source struct {
					File string `xml:"file,attr"`
				} `xml:"source"`
			} `xml:"devices>disk"`
		}{}
		if err := xml.Unmarshal([]byte(domainXML), &info); err != nil {
			panic(err)
		}
		for _, disk := range info.Disk {
			fmt.Println("creating disk", disk.Source.File)
			if err := CreateVolume(conn, poolName, filepath.Base(disk.Source.File)); err != nil {
				return err
			}
		}
	}
	return nil
}

func CreateVolume(conn *libvirt.Connect, poolName, volName string) error {
	pool, err := conn.LookupStoragePoolByName(poolName)
	if err != nil {
		return err
	}
	poolXmlString, err := pool.GetXMLDesc(0)
	if err != nil {
		return err
	}
	poolXml := struct {
		TargetPath string `xml:"target>path"`
	}{}
	if err := xml.Unmarshal([]byte(poolXmlString), &poolXml); err != nil {
		return err
	}
	volXml := fmt.Sprintf(`
    <volume>
        <name>%s</name>
        <key>%s</key>
        <capacity unit="M">10</capacity>
        <allocation unit="M">1</allocation>
        <target>
        	<permissions>
			    <owner>0</owner>
			    <group>0</group>
			    <mode>0777</mode>
			</permissions>
        </target>
    </volume>
    `, volName, filepath.Join(poolXml.TargetPath, volName))
	_, err = pool.StorageVolCreateXML(volXml, 0)
	return err
}

func CreateDomain(conn *libvirt.Connect, name string) (*libvirt.Domain, error) {
	domainXMLPath := fmt.Sprintf("%s/fixtures/libvirt/%s/domain-%s.xml", SourceDir(), os.Getenv(TEST_TYPE_ENV_KEY), name)
	domainXMLConfig, err := ioutil.ReadFile(domainXMLPath)
	if err != nil {
		return nil, err
	}
	domain, err := conn.DomainDefineXML(string(domainXMLConfig))
	if err != nil {
		return nil, err
	}
	if err := domain.Create(); err != nil {
		return nil, err
	}
	return domain, nil
}

func CreateNetwork(conn *libvirt.Connect, name string) (*libvirt.Network, error) {
	networkXMLPath := fmt.Sprintf("%s/fixtures/libvirt/%s/network-%s.xml", SourceDir(), os.Getenv(TEST_TYPE_ENV_KEY), name)
	networkXMLConfig, err := ioutil.ReadFile(networkXMLPath)
	if err != nil {
		return nil, err
	}
	network, err := conn.NetworkDefineXML(string(networkXMLConfig))
	if err != nil {
		return nil, err
	}
	if err := network.Create(); err != nil {
		return nil, err
	}
	return network, nil
}

func CreatePool(conn *libvirt.Connect, name string) (*libvirt.StoragePool, error) {
	poolXMLPath := fmt.Sprintf("%s/fixtures/libvirt/%s/pool-%s.xml", SourceDir(), os.Getenv(TEST_TYPE_ENV_KEY), name)
	poolXMLConfig, err := ioutil.ReadFile(poolXMLPath)
	if err != nil {
		return nil, err
	}

	pool, err := conn.StoragePoolDefineXML(string(poolXMLConfig), 0)
	if err != nil {
		return nil, err
	}
	if err := pool.Create(0); err != nil {
		return nil, err
	}
	return pool, nil
}
