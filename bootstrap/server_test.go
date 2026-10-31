package bootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/config"
	"testing"
	"time"
)

func testTLSFiles(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("cannot trust test certificate")
	}
	return certFile, keyFile, roots
}

func TestWebServerTransports(t *testing.T) {
	certFile, keyFile, roots := testTLSFiles(t)
	for _, secure := range []bool{false, true} {
		scheme := "http"
		if secure {
			scheme = "https"
		}
		t.Run(scheme, func(t *testing.T) {
			cfg := config.APIConfig{Listen: "127.0.0.1:0"}
			if secure {
				cfg.TLSCertFile, cfg.TLSKeyFile = certFile, keyFile
			}
			server, err := newWebServer(&cfg)
			if err != nil {
				t.Fatal(err)
			}
			server.ErrorLog = log.New(io.Discard, "", 0)
			server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if (r.TLS != nil) != secure {
					t.Error("unexpected request transport")
				}
				io.WriteString(w, "vmango")
			})
			listener, err := net.Listen("tcp", server.Addr)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- serveWebServer(server, listener) }()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := server.Shutdown(ctx); err != nil {
					server.Close()
					t.Error(err)
				}
				if err := <-done; err != http.ErrServerClosed {
					t.Errorf("unexpected serve error: %v", err)
				}
			})
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, ForceAttemptHTTP2: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			res, err := client.Get(scheme + "://" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 || string(body) != "vmango" {
				t.Fatalf("unexpected response: %d %q %v", res.StatusCode, body, err)
			}
			if secure {
				if res.TLS == nil || res.TLS.Version < tls.VersionTLS12 || res.ProtoMajor != 2 {
					t.Fatalf("unexpected TLS/HTTP negotiation: %v %s", res.TLS, res.Proto)
				}
				res, err := client.Get("http://" + listener.Addr().String())
				if err == nil {
					res.Body.Close()
					if res.StatusCode != http.StatusBadRequest {
						t.Fatalf("TLS listener accepted plain HTTP: %d", res.StatusCode)
					}
				}
			}
		})
	}
}

func TestWebServerRejectsInvalidTLS(t *testing.T) {
	certFile, keyFile, _ := testTLSFiles(t)
	_, otherKey, _ := testTLSFiles(t)
	broken := filepath.Join(t.TempDir(), "broken.pem")
	if err := os.WriteFile(broken, []byte("invalid PEM"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, cert, key string }{
		{"certificate only", certFile, ""}, {"key only", "", keyFile},
		{"missing certificate", certFile + ".missing", keyFile}, {"missing key", certFile, keyFile + ".missing"},
		{"broken certificate", broken, keyFile}, {"broken key", certFile, broken},
		{"mismatched key", certFile, otherKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if server, err := newWebServer(&config.APIConfig{TLSCertFile: tc.cert, TLSKeyFile: tc.key}); err == nil || server != nil {
				t.Fatalf("invalid TLS configuration accepted: %v", err)
			}
		})
	}
}

func TestWebStartupFailsBeforeCreatingState(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, tc := range []struct{ name, settings, wantError string }{
		{"TLS files", `tls_cert_file = "missing.pem" tls_key_file = "missing.key"`, "load API TLS certificate"},
		{"occupied port", fmt.Sprintf("listen = %q", listener.Addr().String()), "listen on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			state := filepath.Join(dir, "state")
			file := filepath.Join(dir, "vmango.conf")
			content := fmt.Sprintf("key_file = %q\napi { jwt_signing_key_file = %q %s }", filepath.Join(state, "authorized_keys"), filepath.Join(state, "jwt"), tc.settings)
			if err := os.WriteFile(file, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := Web(file); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected startup error %q, got %v", tc.wantError, err)
			}
			if _, err := os.Stat(state); !os.IsNotExist(err) {
				t.Fatalf("startup failure touched state: %v", err)
			}
		})
	}
}
