package bootstrap

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"subuk/vmango/config"
	"subuk/vmango/util"
)

func newWebServer(cfg *config.APIConfig) (*http.Server, error) {
	if err := cfg.ValidateTLS(); err != nil {
		return nil, err
	}
	server := &http.Server{Addr: cfg.Listen}
	if cfg.TLSCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(util.ExpandHomeDir(cfg.TLSCertFile), util.ExpandHomeDir(cfg.TLSKeyFile))
		if err != nil {
			return nil, fmt.Errorf("load API TLS certificate and key: %w", err)
		}
		server.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		}
	}
	return server, nil
}

func serveWebServer(server *http.Server, listener net.Listener) error {
	if server.TLSConfig != nil {
		return server.ServeTLS(listener, "", "")
	}
	return server.Serve(listener)
}
