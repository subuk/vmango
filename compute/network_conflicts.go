package compute

import (
	"fmt"
	"net/netip"
)

// ValidateNATAddressConflicts checks observed target interfaces before creating a network.
// Existing networks are reconciled against their own backend identity separately.
func ValidateNATAddressConflicts(subnet string, addresses []string) error {
	want, err := netip.ParsePrefix(subnet)
	if err != nil {
		return fmt.Errorf("invalid NAT subnet")
	}
	for _, address := range addresses {
		p, err := netip.ParsePrefix(address)
		if err != nil {
			return fmt.Errorf("invalid observed interface prefix")
		}
		if p.Overlaps(want) {
			return fmt.Errorf("%w: NAT subnet %s overlaps target interface prefix %s", ErrConflict, subnet, p.Masked())
		}
	}
	return nil
}
