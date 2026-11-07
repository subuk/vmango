package libvirt

import (
	"net/url"
	"strconv"
)

// graphicsSSHArguments retains the URI's explicit connection settings without
// overriding an omitted user or port supplied by the controller's SSH config.
func graphicsSSHArguments(uri *url.URL, consoleHost string, consolePort int) []string {
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=15"}
	if uri.User != nil && uri.User.Username() != "" {
		args = append(args, "-l", uri.User.Username())
	}
	if port := uri.Port(); port != "" {
		args = append(args, "-p", port)
	}
	return append(args, "--", uri.Hostname(), graphicsForwardCommand(consoleHost, strconv.Itoa(consolePort)))
}
