package libvirt

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestConnectionPoolCloseWaitsForLeasesAndPreventsReopening(t *testing.T) {
	pool := NewConnectionPool(map[string]string{"node": "test:///default"}, []string{"node"}, zerolog.Nop())
	if _, err := pool.Acquire("node"); err != nil {
		t.Skipf("optional libvirt test driver unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- pool.Close() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		pool.cacheMu.RLock()
		closed := pool.closed
		pool.cacheMu.RUnlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			pool.Release("node")
			t.Fatal("Close did not prevent new acquisitions")
		}
		runtime.Gosched()
	}
	select {
	case err := <-done:
		t.Fatalf("Close returned before the outstanding lease was released: %v", err)
	default:
	}
	if _, err := pool.Acquire("node"); !errors.Is(err, ErrConnectionPoolClosed) {
		pool.Release("node")
		t.Fatalf("acquired a closing pool: %v", err)
	}
	pool.Release("node")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	entry := pool.cache["test:///default"]
	entry.Mu.Lock()
	defer entry.Mu.Unlock()
	if entry.Conn != nil {
		t.Fatal("closed pool retained its connection reference")
	}
}

func TestEmptyConnectionPoolCloseIsIdempotent(t *testing.T) {
	pool := NewConnectionPool(map[string]string{"node": "test:///default"}, []string{"node"}, zerolog.Nop())
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Acquire("node"); !errors.Is(err, ErrConnectionPoolClosed) {
		t.Fatalf("closed pool reopened: %v", err)
	}
}
