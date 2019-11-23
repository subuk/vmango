package libvirt

import (
	"subuk/vmango/util"
	"sync"

	"github.com/rs/zerolog"

	libvirt "github.com/libvirt/libvirt-go"
)

type ConnectionPool struct {
	uri      string
	username string
	password string
	mutex    *sync.Mutex
	logger   zerolog.Logger
	cached   *libvirt.Connect
}

func NewConnectionPool(uri, username, password string, logger zerolog.Logger) *ConnectionPool {
	return &ConnectionPool{
		uri:      uri,
		username: username,
		password: password,
		mutex:    &sync.Mutex{},
		logger:   logger,
	}
}

func (p *ConnectionPool) connect() (*libvirt.Connect, error) {
	if p.username == "" && p.password == "" {
		return libvirt.NewConnect(p.uri)
	}
	auth := &libvirt.ConnectAuth{
		CredType: []libvirt.ConnectCredentialType{libvirt.CRED_AUTHNAME, libvirt.CRED_PASSPHRASE},
		Callback: func(creds []*libvirt.ConnectCredential) {
			for _, cred := range creds {
				switch cred.Type {
				default:
				case libvirt.CRED_AUTHNAME:
					cred.Result = p.username
					cred.ResultLen = len(p.username)
				case libvirt.CRED_PASSPHRASE:
					cred.Result = p.password
					cred.ResultLen = len(p.password)
				}
			}
		},
	}
	return libvirt.NewConnectWithAuth(p.uri, auth, 0)
}

func (p *ConnectionPool) Acquire() (*libvirt.Connect, error) {
	p.mutex.Lock()
	var conn *libvirt.Connect

	if p.cached != nil {
		conn = p.cached
	}

	if conn == nil {
		p.logger.Debug().Msg("establishing new connection")
		newConn, err := p.connect()
		if err != nil {
			p.mutex.Unlock()
			return nil, util.NewError(err, "cannot open libvirt connection")
		}
		p.cached = newConn
		return newConn, nil
	}
	alive, err := conn.IsAlive()
	if err != nil {
		newConn, err := p.connect()
		if err != nil {
			p.mutex.Unlock()
			return nil, util.NewError(err, "cannot reopen libvirt connection")
		}
		p.cached = newConn
		return newConn, nil
	}
	if !alive {
		newConn, err := p.connect()
		if err != nil {
			p.mutex.Unlock()
			return nil, util.NewError(err, "cannot reopen libvirt connection")
		}
		p.cached = newConn
		return newConn, nil
	}
	return conn, nil
}

func (p *ConnectionPool) Release(conn *libvirt.Connect) {
	p.mutex.Unlock()
}
