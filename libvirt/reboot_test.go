package libvirt

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestWaitForRebootTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := waitForReboot(make(chan struct{}))
		if elapsed := time.Since(start); elapsed != 10*time.Second {
			t.Fatalf("waited %s, want 10s", elapsed)
		}
		if err == nil || !strings.Contains(err.Error(), "within 10s; the backend outcome is uncertain") {
			t.Fatalf("expected timeout with uncertain outcome, got %v", err)
		}
	})
}

func TestWaitForRebootConfirmation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		observed := make(chan struct{}, 1)
		go func() {
			time.Sleep(time.Second)
			observed <- struct{}{}
		}()
		start := time.Now()
		if err := waitForReboot(observed); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("waited %s, want immediate return on confirmation after 1s", elapsed)
		}
	})
}
