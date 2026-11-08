package libvirt

import (
	"fmt"
	xmlmodel "libvirt.org/go/libvirtxml"
)

func uintPointer(v uint) *uint { return &v }
func valueOrZero(v *uint) uint {
	if v == nil {
		return 0
	}
	return *v
}
func driveAddressKey(a *xmlmodel.DomainAddressDrive) string {
	return fmt.Sprintf("%d/%d/%d/%d", valueOrZero(a.Controller), valueOrZero(a.Bus), valueOrZero(a.Target), valueOrZero(a.Unit))
}

// diskTargetIndex supports the complete alphabetic target suffix, including vdaa.
func diskTargetIndex(name, prefix string) (int, bool) {
	if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
		return 0, false
	}
	index := 0
	for _, ch := range name[len(prefix):] {
		if ch < 'a' || ch > 'z' {
			return 0, false
		}
		index = index*26 + int(ch-'a') + 1
		if index > 1<<20 {
			return 0, false
		}
	}
	return index - 1, true
}
func diskTargetName(index int) string {
	out := ""
	for index >= 0 {
		out = string(rune('a'+index%26)) + out
		index = index/26 - 1
	}
	return out
}
