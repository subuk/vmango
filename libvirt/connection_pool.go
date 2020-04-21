package libvirt

import (
	"subuk/vmango/compute"
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
	cacheMu   *sync.RWMutex
}

func NewConnectionPool(nodeUri map[string]string, nodeOrder []string, logger zerolog.Logger) *ConnectionPool {
	return &ConnectionPool{
		nodeUri:   nodeUri,
		nodeOrder: nodeOrder,
		cache:     map[string]*connection{},
		cacheMu:   &sync.RWMutex{},
		logger:    logger,
	}
}

func (p *ConnectionPool) Nodes(only []string) []string {
	result := []string{}
	for _, node := range p.nodeOrder {
		if _, ok := p.nodeUri[node]; !ok {
			continue
		}
		if len(only) == 0 {
			result = append(result, node)
			continue
		}
		for _, needle := range only {
			if needle != node {
				continue
			}
			result = append(result, node)
		}

	}
	return result
}

func (p *ConnectionPool) Acquire(node string) (*libvirt.Connect, error) {
	if node == "" {
		panic("empty node id")
	}
	p.cacheMu.Lock()
	uri, nodeExists := p.nodeUri[node]
	if !nodeExists {
		p.cacheMu.Unlock()
		return nil, compute.ErrUnknownNode
	}
	if p.cache[uri] == nil {
		p.cache[uri] = &connection{Mu: &sync.Mutex{}}
	}
	p.cacheMu.Unlock()
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()

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
