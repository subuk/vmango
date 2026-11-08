package libvirt

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"

	vir "libvirt.org/go/libvirt"
)

func TestShutdownConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delay time.Duration
	}{
		{"immediate", 0},
		{"delayed", 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				observed := make(chan struct{}, 1)
				callback := shutdownObserver(observed)
				confirm := func() {
					callback(nil, nil, &vir.DomainEventLifecycle{Event: vir.DOMAIN_EVENT_STOPPED, Detail: int(vir.DOMAIN_EVENT_STOPPED_SHUTDOWN)})
				}
				if tc.delay == 0 {
					confirm()
					confirm() // Repeated events must not block the event loop.
				} else {
					go func() { time.Sleep(tc.delay); confirm() }()
				}
				start := time.Now()
				if err := waitForMachineEvent(observed, "shutdown", "stopped (shutdown)"); err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(start); elapsed != tc.delay {
					t.Fatalf("waited %s, want %s", elapsed, tc.delay)
				}
			})
		})
	}
}

func TestShutdownConfirmationTimeout(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event *vir.DomainEventLifecycle
	}{
		{"no event", nil},
		{"shutdown started", &vir.DomainEventLifecycle{Event: vir.DOMAIN_EVENT_SHUTDOWN}},
		{"suspended", &vir.DomainEventLifecycle{Event: vir.DOMAIN_EVENT_SUSPENDED}},
		{"crashed", &vir.DomainEventLifecycle{Event: vir.DOMAIN_EVENT_STOPPED, Detail: int(vir.DOMAIN_EVENT_STOPPED_CRASHED)}},
		{"migrated", &vir.DomainEventLifecycle{Event: vir.DOMAIN_EVENT_STOPPED, Detail: int(vir.DOMAIN_EVENT_STOPPED_MIGRATED)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				observed := make(chan struct{}, 1)
				if tc.event != nil {
					shutdownObserver(observed)(nil, nil, tc.event)
				}
				start := time.Now()
				err := waitForMachineEvent(observed, "shutdown", "stopped (shutdown)")
				if elapsed := time.Since(start); elapsed != 120*time.Second {
					t.Fatalf("waited %s, want 120s", elapsed)
				}
				if err == nil || !strings.Contains(err.Error(), "shutdown was requested") || !strings.Contains(err.Error(), "within 2m0s; the backend outcome is uncertain") {
					t.Fatalf("expected shutdown timeout with uncertain outcome, got %v", err)
				}
			})
		})
	}
}
