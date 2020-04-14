package libvirt

import (
	"subuk/vmango/util"
	"sync"

	"github.com/rs/zerolog"

	libvirt "github.com/libvirt/libvirt-go"
)

type connection struct {
	Conn *libvirt.Connect
	Mu   *sync.Mutex
}

type ConnectionPool struct {
	nodeUri   map[string]string
	nodeOrder []string
	logger    zerolog.Logger
	cache     map[string]*connection
}

func NewConnectionPool(nodeUri map[string]string, nodeOrder []string, logger zerolog.Logger) *ConnectionPool {
	return &ConnectionPool{
		nodeUri:   nodeUri,
		nodeOrder: nodeOrder,
		cache:     map[string]*connection{},
		logger:    logger,
	}
}
func (p *ConnectionPool) Nodes() []string {
	return p.nodeOrder
}
func (p *ConnectionPool) Acquire(node string) (*libvirt.Connect, error) {
	if node == "" {
		panic("empty node id")
	}
	uri := p.nodeUri[node]
	if p.cache[uri] == nil {
		p.cache[uri] = &connection{Mu: &sync.Mutex{}}
	}

	p.cache[uri].Mu.Lock()
	if p.cache[uri].Conn == nil {
		p.logger.Debug().Str("uri", uri).Msg("establishing new connection")
		newConn, err := libvirt.NewConnect(uri)
		if err != nil {
			p.cache[uri].Mu.Unlock()
			return nil, util.NewError(err, "cannot open libvirt connection")
		}
		p.cache[uri].Conn = newConn
		return newConn, nil
	}
	alive, err := p.cache[uri].Conn.IsAlive()
	if err != nil {
		newConn, err := libvirt.NewConnect(uri)
		if err != nil {
			p.cache[uri].Mu.Unlock()
			return nil, util.NewError(err, "cannot reopen libvirt connection")
		}
		p.cache[uri].Conn = newConn
		return newConn, nil
	}
	if !alive {
		newConn, err := libvirt.NewConnect(uri)
		if err != nil {
			p.cache[uri].Mu.Unlock()
			return nil, util.NewError(err, "cannot reopen libvirt connection")
		}
		p.cache[uri].Conn = newConn
		return newConn, nil
	}
	return p.cache[uri].Conn, nil
}

func (p *ConnectionPool) Release(node string) {
	uri := p.nodeUri[node]
	p.cache[uri].Mu.Unlock()
}
