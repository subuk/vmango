package libvirt

import (
	"subuk/vmango/compute"
	"sync"
	"time"

	"github.com/rs/zerolog"

	libvirt "libvirt.org/go/libvirt"
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

var eventLoopOnce sync.Once
var eventLoopError error

func ensureEventLoop() error {
	eventLoopOnce.Do(func() {
		eventLoopError = libvirt.EventRegisterDefaultImpl()
		if eventLoopError == nil {
			go func() {
				for {
					if libvirt.EventRunDefaultImpl() != nil {
						time.Sleep(time.Second)
					}
				}
			}()
		}
	})
	return eventLoopError
}

func NewConnectionPool(nodeUri map[string]string, nodeOrder []string, logger zerolog.Logger) *ConnectionPool {
	_ = ensureEventLoop()
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
	entry := p.cache[uri]
	p.cacheMu.Unlock()

	entry.Mu.Lock()
	if entry.Conn == nil {
		p.logger.Debug().Str("node", node).Msg("establishing new connection")
		newConn, err := libvirt.NewConnect(uri)
		if err != nil {
			entry.Mu.Unlock()
			return nil, repositoryError(err, "cannot open libvirt connection")
		}
		entry.Conn = newConn
		return newConn, nil
	}
	alive, err := entry.Conn.IsAlive()
	if err != nil {
		newConn, err := libvirt.NewConnect(uri)
		if err != nil {
			entry.Mu.Unlock()
			return nil, repositoryError(err, "cannot reopen libvirt connection")
		}
		entry.Conn = newConn
		return newConn, nil
	}
	if !alive {
		newConn, err := libvirt.NewConnect(uri)
		if err != nil {
			entry.Mu.Unlock()
			return nil, repositoryError(err, "cannot reopen libvirt connection")
		}
		entry.Conn = newConn
		return newConn, nil
	}
	return entry.Conn, nil
}

func (p *ConnectionPool) Release(node string) {
	p.cacheMu.RLock()
	entry := p.cache[p.nodeUri[node]]
	p.cacheMu.RUnlock()
	entry.Mu.Unlock()
}
