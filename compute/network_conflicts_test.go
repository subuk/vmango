package compute

import "testing"

func TestNATAddressConflicts(t *testing.T) {
	for _, tc := range []struct {
		addresses []string
		bad       bool
	}{{[]string{"192.168.32.209/24", "fe80::1/64"}, false}, {[]string{"192.168.177.20/24"}, true}, {[]string{"192.168.0.1/16"}, true}, {[]string{"malformed"}, true}} {
		if err := ValidateNATAddressConflicts("192.168.177.0/24", tc.addresses); (err != nil) != tc.bad {
			t.Fatalf("%v: %v", tc.addresses, err)
		}
	}
}
