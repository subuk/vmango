package compute

import "strings"

// GuestHostname is independent of display labels and generated backend names.
// An explicit hostname is validated; a label supplies only a safe default.
func GuestHostname(explicit, label, fallback string) (string, error) {
	if explicit != "" {
		if len(explicit) > 253 {
			return "", invalid("hostname", "invalid guest hostname")
		}
		for _, part := range strings.Split(explicit, ".") {
			if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
				return "", invalid("hostname", "invalid guest hostname")
			}
			for _, c := range part {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return "", invalid("hostname", "invalid guest hostname")
				}
			}
		}
		return explicit, nil
	}
	var name strings.Builder
	for _, c := range strings.ToLower(label) {
		if name.Len() >= 63 {
			break
		}
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			name.WriteRune(c)
		} else {
			name.WriteByte('-')
		}
	}
	value := strings.Trim(name.String(), "-")
	if value == "" {
		value = fallback
	}
	return value, nil
}
