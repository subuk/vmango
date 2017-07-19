package dal

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"vmango/cfg"
	"vmango/models"

	"github.com/Sirupsen/logrus"
	"github.com/lxc/lxd/client"
	lxd_shared "github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/api"
)

const LXD_PROVIDER_TYPE = "lxd"

type LXDProvider struct {
	name string
	conn lxd.ContainerServer
	url  string

	machines *LXDMachinerep
	images   *LXDImagerep
}

func getRemoteCertificate(address string) (*x509.Certificate, error) {
	// Setup a permissive TLS config
	tlsConfig, err := lxd_shared.GetTLSConfig("", "", "", nil)
	if err != nil {
		return nil, err
	}

	tlsConfig.InsecureSkipVerify = true
	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
		Dial:            lxd_shared.RFC3493Dialer,
		Proxy:           lxd_shared.ProxyFromEnvironment,
	}

	// Connect
	client := &http.Client{Transport: tr}
	resp, err := client.Get(address)
	if err != nil {
		return nil, err
	}

	// Retrieve the certificate
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return nil, fmt.Errorf("Unable to read remote TLS certificate")
	}

	return resp.TLS.PeerCertificates[0], nil
}

func NewLXDProvider(conf cfg.LXDConfig) (*LXDProvider, error) {
	if strings.HasPrefix(conf.Url, "unix://") {
		conf.Url = conf.Url[7:]
		return NewLXDProviderUnix(conf)
	} else {
		return NewLXDProviderRemote(conf)
	}
}

func NewLXDProviderUnix(conf cfg.LXDConfig) (*LXDProvider, error) {
	conn, err := lxd.ConnectLXDUnix(conf.Url, &lxd.ConnectionArgs{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to local lxd server on path '%s': %s", conf.Url, err)
	}
	logrus.WithFields(logrus.Fields{
		"provider":    conf.Name,
		"socket_path": conf.Url,
	}).Info("successfully authenticated on local lxd server")

	return &LXDProvider{
		name:     conf.Name,
		conn:     conn,
		url:      conf.Url,
		images:   NewLXDImagerep(conn),
		machines: &LXDMachinerep{conn: conn},
	}, nil
}

func NewLXDProviderRemote(conf cfg.LXDConfig) (*LXDProvider, error) {
	if !lxd_shared.PathExists(conf.Cert) || !lxd_shared.PathExists(conf.Key) {
		logrus.WithField("provider", conf.Name).Info(
			"generating a client certificate. This may take a minute...",
		)
		if err := lxd_shared.FindOrGenCert(conf.Cert, conf.Key, true); err != nil {
			return nil, err
		}
	}
	if !lxd_shared.PathExists(conf.ServerCert) {
		serverCert, err := getRemoteCertificate(conf.Url)
		if err != nil {
			return nil, err
		}
		f, err := os.Create(conf.ServerCert)
		if err != nil {
			return nil, err
		}
		if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: serverCert.Raw}); err != nil {
			return nil, err
		}
		f.Close()
	}

	serverCertB, err := ioutil.ReadFile(conf.ServerCert)
	if err != nil {
		return nil, fmt.Errorf("failed to read server cert: %s", err)
	}
	clientCertB, err := ioutil.ReadFile(conf.Cert)
	if err != nil {
		return nil, fmt.Errorf("failed to read client cert: %s", err)
	}
	clientKeyB, err := ioutil.ReadFile(conf.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to read client key: %s", err)
	}

	conn, err := lxd.ConnectLXD(conf.Url, &lxd.ConnectionArgs{
		TLSClientCert: string(clientCertB),
		TLSClientKey:  string(clientKeyB),
		TLSServerCert: string(serverCertB),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to lxd: %s", err)
	}

	srv, _, err := conn.GetServer()
	if err != nil {
		return nil, fmt.Errorf("failed to get server: %s", err)
	}
	if srv.Auth != "trusted" {
		logrus.WithField("provider", conf.Name).Info("adding out certificate to trusted on server")
		req := api.CertificatesPost{
			Password: conf.Password,
		}
		req.Type = "client"

		if err := conn.CreateCertificate(req); err != nil {
			return nil, fmt.Errorf("failed to create certificate: %s", err)
		}
		srv, _, err = conn.GetServer()
		if err != nil {
			return nil, fmt.Errorf("failed to get server: %s", err)
		}
		if srv.Auth != "trusted" {
			return nil, fmt.Errorf("server still doesn't trust us")
		}
	}
	logrus.WithField("provider", conf.Name).Info("successfully authenticated on lxd server")
	return &LXDProvider{
		name:     conf.Name,
		conn:     conn,
		url:      conf.Url,
		images:   NewLXDImagerep(conn),
		machines: &LXDMachinerep{conn: conn},
	}, nil
}

func (p *LXDProvider) Images() Imagerep {
	return p.images
}

func (p *LXDProvider) Machines() Machinerep {
	return p.machines
}

func (p *LXDProvider) Status(status *models.StatusInfo) error {
	status.Name = p.Name()
	status.Type = LXD_PROVIDER_TYPE
	server, _, err := p.conn.GetServer()
	if err != nil {
		return fmt.Errorf("failed to fetch server info: %s", err)
	}
	status.Connection = p.url
	status.Description = fmt.Sprintf(
		"lxd %s over %s %s on %s %s",
		server.Environment.ServerVersion,
		server.Environment.Driver,
		server.Environment.DriverVersion,
		server.Environment.Kernel,
		server.Environment.KernelVersion,
	)

	cts, err := p.conn.GetContainers()
	if err != nil {
		return fmt.Errorf("failed to fetch containers: %s", err)
	}
	status.MachineCount = len(cts)
	return nil
}

func (p *LXDProvider) Name() string {
	return p.name
}
