package libvirt

import (
	"subuk/vmango/util"
	"sync"

	"github.com/rs/zerolog"

	libvirt "github.com/libvirt/libvirt-go"
)

type ConnectionPool struct {
	uri    string
	mutex  *sync.Mutex
	logger zerolog.Logger
	cached *libvirt.Connect
}

func NewConnectionPool(uri string, logger zerolog.Logger) *ConnectionPool {
	return &ConnectionPool{
		uri:    uri,
		mutex:  &sync.Mutex{},
		logger: logger,
	}
}

func (p *ConnectionPool) Acquire() (*libvirt.Connect, error) {
	p.mutex.Lock()
	var conn *libvirt.Connect

	if p.cached != nil {
		conn = p.cached
	}

	if conn == nil {
		p.logger.Debug().Msg("establishing new connection")
		newConn, err := libvirt.NewConnect(p.uri)
		if err != nil {
			p.mutex.Unlock()
			return nil, util.NewError(err, "cannot open libvirt connection")
		}
		p.cached = newConn
		return newConn, nil
	}
	alive, err := conn.IsAlive()
	if err != nil {
		newConn, err := libvirt.NewConnect(p.uri)
		if err != nil {
			p.mutex.Unlock()
			return nil, util.NewError(err, "cannot reopen libvirt connection")
		}
		p.cached = newConn
		return newConn, nil
	}
	if !alive {
		newConn, err := libvirt.NewConnect(p.uri)
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
