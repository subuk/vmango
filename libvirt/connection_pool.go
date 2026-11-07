package libvirt

import (
	"errors"
	"sync"
	"time"

	"subuk/vmango/compute"

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
	closed    bool
	closeDone chan struct{}
	closeErr  error
}

var ErrConnectionPoolClosed = errors.New("libvirt connection pool is closed")

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
		closeDone: make(chan struct{}),
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
	if p.closed {
		p.cacheMu.Unlock()
		return nil, ErrConnectionPoolClosed
	}
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
	// Close may have started while this caller waited for an existing lease.
	p.cacheMu.RLock()
	closed := p.closed
	p.cacheMu.RUnlock()
	if closed {
		entry.Mu.Unlock()
		return nil, ErrConnectionPoolClosed
	}
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
	if err != nil || !alive {
		newConn, err := libvirt.NewConnect(uri)
		if err != nil {
			entry.Mu.Unlock()
			return nil, repositoryError(err, "cannot reopen libvirt connection")
		}
		if _, closeErr := entry.Conn.Close(); closeErr != nil {
			p.logger.Warn().Err(closeErr).Str("node", node).Msg("cannot release replaced libvirt connection")
		}
		entry.Conn = newConn
		return newConn, nil
	}
	return entry.Conn, nil
}

// Close prevents new acquisitions and waits for outstanding leases before
// releasing the pool's connection references. It is safe to call repeatedly.
func (p *ConnectionPool) Close() error {
	p.cacheMu.Lock()
	if p.closed {
		done := p.closeDone
		p.cacheMu.Unlock()
		<-done
		return p.closeErr
	}
	p.closed = true
	entries := make([]*connection, 0, len(p.cache))
	for _, entry := range p.cache {
		entries = append(entries, entry)
	}
	p.cacheMu.Unlock()
	var failures []error
	for _, entry := range entries {
		entry.Mu.Lock()
		if entry.Conn != nil {
			if _, err := entry.Conn.Close(); err != nil {
				failures = append(failures, err)
			}
			entry.Conn = nil
		}
		entry.Mu.Unlock()
	}
	p.cacheMu.Lock()
	p.closeErr = errors.Join(failures...)
	close(p.closeDone)
	p.cacheMu.Unlock()
	return p.closeErr
}

func (p *ConnectionPool) Release(node string) {
	p.cacheMu.RLock()
	entry := p.cache[p.nodeUri[node]]
	p.cacheMu.RUnlock()
	entry.Mu.Unlock()
}
