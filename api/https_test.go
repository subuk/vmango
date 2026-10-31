package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"subuk/vmango/compute"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

func TestNativeHTTPSAuthenticationAndOrigins(t *testing.T) {
	cfg := apiTestConfig(t)
	// Neither proxy settings nor a public URL should be needed for Secure cookies.
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	server := httptest.NewTLSServer(h)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	for _, tc := range []struct {
		name, method, path, origin, site string
		status                           int
	}{
		{"same origin login", "POST", "/api/v1/auth/login", server.URL, "same-origin", 200},
		{"headerless login", "POST", "/api/v1/auth/login", "", "", 200},
		{"cross origin login", "POST", "/api/v1/auth/login", "https://evil.test", "", 403},
		{"cross site login", "POST", "/api/v1/auth/login", "", "cross-site", 403},
		{"HTTP origin", "POST", "/api/v1/auth/login", strings.Replace(server.URL, "https:", "http:", 1), "same-origin", 403},
		{"protected headerless request", "GET", "/api/v1/me", "", "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(`{"username":"admin","password":"secret"}`))
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			res, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				body, _ := io.ReadAll(res.Body)
				t.Fatalf("status %d: %s", res.StatusCode, body)
			}
			if tc.status != 200 {
				return
			}
			cookies := res.Cookies()
			if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
				t.Fatalf("unsafe native HTTPS cookies: %+v", cookies)
			}
			var credentials struct {
				AccessToken string `json:"access_token"`
			}
			if err := json.NewDecoder(res.Body).Decode(&credentials); err != nil {
				t.Fatal(err)
			}
			r, _ = http.NewRequest("GET", server.URL+"/api/v1/me", nil)
			r.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
			me, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			me.Body.Close()
			if me.StatusCode != 200 {
				t.Fatalf("authenticated HTTPS request: %d", me.StatusCode)
			}
		})
	}
}

func TestNativeWSSConsole(t *testing.T) {
	cfg := apiTestConfig(t)
	repo := &apiVMRepo{vm: compute.VirtualMachine{State: compute.StateRunning, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc}}}
	repo.stream = func(bool) (compute.VirtualMachineConsoleStream, error) {
		client, backend := net.Pipe()
		go func() {
			defer backend.Close()
			io.Copy(backend, backend)
		}()
		return client, nil
	}
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, compute.NewVirtualMachineService(repo), nil, nil)
	server := httptest.NewTLSServer(h)
	defer server.Close()
	e := &Environ{cfg: &cfg.API}
	token, err := e.signConsole(consoleClaim{2, "console", "admin", "local", "v", "vnc", false, time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	dialer := websocket.Dialer{
		TLSClientConfig:  server.Client().Transport.(*http.Transport).TLSClientConfig,
		Subprotocols:     []string{"binary", "vmango.token." + token},
		HandshakeTimeout: 3 * time.Second,
	}
	endpoint := "wss" + strings.TrimPrefix(server.URL, "https") + "/console/local/v/vnc/ws"
	for _, origin := range []string{"https://evil.test", strings.Replace(server.URL, "https:", "http:", 1)} {
		ws, res, err := dialer.Dial(endpoint, http.Header{"Origin": []string{origin}})
		if err == nil {
			ws.Close()
			t.Fatalf("accepted invalid WSS origin %q", origin)
		}
		if res == nil || res.StatusCode != 403 {
			t.Fatalf("unexpected origin rejection: %v %v", res, err)
		}
		res.Body.Close()
	}
	ws, res, err := dialer.Dial(endpoint, http.Header{"Origin": []string{server.URL}})
	if err != nil {
		t.Fatalf("WSS upgrade: %v %v", res, err)
	}
	defer ws.Close()
	if ws.Subprotocol() != "binary" {
		t.Fatal("console credential reflected in selected subprotocol")
	}
	payload := []byte{0, 1, 255}
	if err := ws.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatal(err)
	}
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	kind, data, err := ws.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(data, payload) {
		t.Fatalf("WSS console data: %v %v", data, err)
	}
}
