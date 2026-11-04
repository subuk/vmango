package compute

import "testing"

func TestNamespaceGuestHostnameIndependentOfLabel(t *testing.T) {
	for _, tt := range []struct{ explicit, label, want string }{{"guest.example", "Display Label", "guest.example"}, {"", "Display Label", "display-label"}, {"", "Пример", "vm-fallback"}} {
		got, err := GuestHostname(tt.explicit, tt.label, "vm-fallback")
		if err != nil || got != tt.want {
			t.Fatal(tt, got, err)
		}
	}
	for _, bad := range []string{"invalid guest", "-invalid", "a..b", "ümlaut", "a_underscore"} {
		if _, err := GuestHostname(bad, "label", "fallback"); err == nil {
			t.Fatal("accepted invalid hostname", bad)
		}
	}
}
