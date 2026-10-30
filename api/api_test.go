package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/tasks"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"path/filepath"
	"subuk/vmango/auth"
	"subuk/vmango/filesystem"
)

func apiTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg.API.Users = []config.UserAPIConfig{{Id: "admin", HashedPassword: string(hash)}}
	cfg.API.JWTSigningKey = testJWTKey
	cfg.API.ConsoleSigningKey = "938f251d85b54115ae5466f999682d803c8ad5926ba4b11ee7cd123a69cf790ed"
	return cfg
}
func apiRequest(h http.Handler, method, path, body string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://vmango.test"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if method != "GET" && strings.Contains(path, "/clouds/") {
		r.Header.Set("Idempotency-Key", uuid.NewString())
	}
	if auth {
		r.Header.Set("Authorization", "Bearer "+testWebToken())
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAPIAuthenticationAndOrigins(t *testing.T) {
	cfg := apiTestConfig(t)
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	for _, path := range []string{"/api/openapi.json", "/api/v1/hosts", "/api/v1/clouds/local/machines/n/v/consoles/serial/ws"} {
		w := apiRequest(h, "GET", path, "", false)
		if w.Code != 401 || !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), "Bearer") || w.Header().Get("Location") != "" {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	for _, tt := range []struct {
		origin, fetch string
		want          int
	}{
		{"https://vmango.test", "same-origin", 200}, {"", "", 200}, {"https://evil.test", "", 403}, {"", "cross-site", 403}, {"https://sub.vmango.test", "same-site", 403}, {"http://vmango.test", "", 403}, {"null", "", 403},
	} {
		r := httptest.NewRequest("GET", "https://vmango.test/api/openapi.json", nil)
		r.Header.Set("Authorization", "Bearer "+testWebToken())
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Sec-Fetch-Site", tt.fetch)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("%+v: %d %s", tt, w.Code, w.Body)
		}
	}
	for _, credential := range []string{"cookie", "basic"} {
		r := httptest.NewRequest("GET", "https://vmango.test/api/openapi.json", nil)
		if credential == "cookie" {
			r.AddCookie(&http.Cookie{Name: "vmango", Value: "old-session"})
			r.AddCookie(&http.Cookie{Name: refreshCookie, Value: "ignored"})
		} else {
			r.SetBasicAuth("admin", "secret")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("legacy credential authorized API", credential, w.Code)
		}
	}
	for _, tt := range []struct {
		host, peer string
		want       int
	}{{"vmango.test", "192.0.2.1:1234", 403}, {"localhost", "127.0.0.1:1234", 200}, {"localhost", "192.0.2.1:1234", 403}, {"127.0.0.1", "127.0.0.1:1234", 200}} {
		r := httptest.NewRequest("GET", "http://"+tt.host+"/api/docs", nil)
		r.RemoteAddr = tt.peer
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("%+v: %d", tt, w.Code)
		}
	}
	cfg.API.APIDisabled = true
	if _, err := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil, testAuthService(t, cfg), nil); err == nil {
		t.Fatal("silently enabled disabled API")
	}
}

func TestAPISpecificationContract(t *testing.T) {
	cfg := apiTestConfig(t)
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	w := apiRequest(h, "GET", "/api/openapi.json", "", true)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var spec struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string                     `json:"operationId"`
			Responses   map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1.") {
		t.Fatal(spec.OpenAPI)
	}
	ids := map[string]bool{}
	for path, ops := range spec.Paths {
		if strings.HasPrefix(path, "/api/v1/nodes") || path == "/api/v1/vms" || path == "/api/v1/volumes" || path == "/api/v1/networks" {
			t.Fatalf("retired infrastructure route in OpenAPI: %s", path)
		}
		for method, op := range ops {
			if strings.HasPrefix(path, "/api/v1/hosts") && method != "get" {
				t.Fatalf("host diagnostics must be read-only: %s %s", method, path)
			}
			if op.OperationID == "" || ids[op.OperationID] {
				t.Fatalf("duplicate/missing operation id %s", path)
			}
			ids[op.OperationID] = true
			if len(op.Responses) == 0 {
				t.Fatal("missing responses")
			}
			if !strings.HasPrefix(path, "/api/v1/auth/") {
				if r := apiRequest(h, strings.ToUpper(method), strings.NewReplacer("{cloud}", "local", "{node}", "n", "{vm}", "v", "{volume}", "dm9s", "{kind}", "serial", "{mac}", "aa", "{fingerprint}", "ff").Replace(path), "", false); r.Code != 401 {
					t.Fatalf("unguarded %s %s: %d", method, path, r.Code)
				}
			}
		}
	}
	for _, id := range []string{"create-cloud-machine", "list-clouds", "get-cloud", "list-cloud-machines", "change-instance-type", "adopt-machine", "create-machine-disk", "update-vm", "get-vm", "delete-vm", "vm-action", "attach-disk", "detach-disk", "attach-interface", "detach-interface", "clone-volume", "resize-volume", "delete-volume", "get-volume", "list-keys", "get-key", "add-key", "delete-key", "list-hosts", "get-host", "list-networks", "list-pools", "list-images", "get-capabilities", "create-console-link", "connect-console"} {
		if !ids[id] {
			t.Errorf("missing operation %s", id)
		}
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, w.Body.Bytes(), "", "  "); err != nil {
		t.Fatal(err)
	}
	pretty.WriteByte('\n')
	const path = "testdata/openapi.json"
	if os.Getenv("UPDATE_API_SPEC") == "1" {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, pretty.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, pretty.Bytes()) {
		t.Fatal("API contract changed: review changes, then UPDATE_API_SPEC=1 make test")
	}
	docs := apiRequest(h, "GET", "/api/docs", "", true)
	if docs.Code != 200 || strings.Contains(docs.Body.String(), "https://") {
		t.Fatal("documentation must work offline without validator uploads")
	}
}

type apiVMRepo struct {
	compute.VirtualMachineRepository
	mu     sync.Mutex
	vm     compute.VirtualMachine
	getErr error
	saves  int
	stream func(bool) (compute.VirtualMachineConsoleStream, error)
}

func (r *apiVMRepo) Get(id, node string) (*compute.VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	v := r.vm
	v.Id = id
	v.NodeId = node
	return &v, nil
}
func (r *apiVMRepo) Save(v *compute.VirtualMachine) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vm = *v
	r.saves++
	return nil
}
func (r *apiVMRepo) GetConsoleStream(id, node string, force bool) (compute.VirtualMachineConsoleStream, error) {
	return r.stream(force)
}
func (r *apiVMRepo) GetGraphicStream(id, node string) (compute.VirtualMachineGraphicStream, error) {
	s, err := r.stream(false)
	return s, err
}
func TestAPIPatchAndErrors(t *testing.T) {
	cfg := apiTestConfig(t)
	repo := &apiVMRepo{vm: compute.VirtualMachine{VCpus: 4, Memory: compute.NewSize(2, compute.SizeUnitG), GuestAgent: true, Autostart: true, Hugepages: true, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc}, VideoModel: compute.VideoModelNone}}
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, compute.NewVirtualMachineService(repo), nil, nil)
	w := apiRequest(h, "PATCH", "/api/v1/clouds/local/machines/n/v", `{"autostart":false}`, true)
	if w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	awaitAPITask(t, h, w)
	if repo.vm.Autostart || !repo.vm.GuestAgent || !repo.vm.Hugepages || repo.vm.VCpus != 4 || repo.vm.Memory.Bytes() != 2<<30 {
		t.Fatalf("omitted fields changed: %+v", repo.vm)
	}
	for _, body := range []string{`{"vcpus":0}`, `{"graphics":"bad"}`, `{"memory_bytes":-1}`, `{"unexpected":true}`, `broken`} {
		w := apiRequest(h, "PATCH", "/api/v1/clouds/local/machines/n/v", body, true)
		if w.Code != 422 && w.Code != 400 {
			t.Fatalf("invalid %s: %d", body, w.Code)
		}
	}
	if repo.saves != 1 {
		t.Fatalf("invalid requests mutated VM %d", repo.saves)
	}
	for _, tt := range []struct {
		err    error
		status int
	}{{compute.ErrNotFound, 404}, {compute.ErrConflict, 409}, {compute.ErrUnavailable, 503}, {fmt.Errorf("secret backend data"), 500}} {
		repo.getErr = tt.err
		w := apiRequest(h, "GET", "/api/v1/clouds/local/machines/n/v", "", true)
		if w.Code != tt.status || !strings.Contains(w.Header().Get("Content-Type"), "problem+json") || strings.Contains(w.Body.String(), "secret backend") {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
}

func TestConsoleTokenBinding(t *testing.T) {
	cfg := apiTestConfig(t)
	e := &Environ{cfg: &cfg.API}
	now := time.Now()
	base := consoleClaim{1, "console", "admin", "node", "vm", "serial", false, now.Add(time.Hour).Unix()}
	token, err := e.signConsole(base)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := e.verifyConsole(token, "node", "vm", "serial", now); err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []func(*consoleClaim){func(c *consoleClaim) { c.Version = 2 }, func(c *consoleClaim) { c.Purpose = "login" }, func(c *consoleClaim) { c.Node = "other" }, func(c *consoleClaim) { c.VM = "other" }, func(c *consoleClaim) { c.Kind = "vnc" }, func(c *consoleClaim) { c.User = "" }, func(c *consoleClaim) { c.Expires = now.Unix() }} {
		c := base
		change(&c)
		token, _ := e.signConsole(c)
		if _, err := e.verifyConsole(token, "node", "vm", "serial", now); err == nil {
			t.Fatalf("accepted invalid claim %+v", c)
		}
	}
	for _, bad := range []string{token + "x", "x" + token, "", strings.Repeat("a", 9000)} {
		if _, err := e.verifyConsole(bad, "node", "vm", "serial", now); err == nil {
			t.Fatal("accepted tampering")
		}
	}
	cfg.API.ConsoleSigningKey = strings.Repeat("k", 32)
	if _, err := e.verifyConsole(token, "node", "vm", "serial", now); err == nil {
		t.Fatal("rotation did not invalidate link")
	}
}

func TestConsoleStreamsAndExpiry(t *testing.T) {
	cfg := apiTestConfig(t)
	repo := &apiVMRepo{vm: compute.VirtualMachine{State: compute.StateRunning, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc}}}
	var peersMu sync.Mutex
	peers := []net.Conn{}
	forces := []bool{}
	repo.stream = func(force bool) (compute.VirtualMachineConsoleStream, error) {
		a, b := net.Pipe()
		peersMu.Lock()
		peers = append(peers, b)
		forces = append(forces, force)
		peersMu.Unlock()
		go io.Copy(b, b)
		return a, nil
	}
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, compute.NewVirtualMachineService(repo), nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer func() {
		for _, p := range peers {
			p.Close()
		}
	}()
	e := &Environ{cfg: &cfg.API}
	expires := time.Now().Add(3 * time.Second).Truncate(time.Second)
	token, _ := e.signConsole(consoleClaim{1, "console", "admin", "n", "v", "serial", true, expires.Unix()})
	endpoint := "ws" + strings.TrimPrefix(srv.URL, "http") + "/console/n/v/serial/ws"
	dial := websocket.Dialer{Subprotocols: []string{"binary", "vmango.token." + token}}
	for i := 0; i < 2; i++ {
		ws, res, err := dial.Dial(endpoint, nil)
		if err != nil {
			t.Fatalf("dial %v %v", err, res)
		}
		if ws.Subprotocol() != "binary" {
			t.Fatal("credential reflected in selected subprotocol")
		}
		ws.WriteMessage(websocket.BinaryMessage, []byte{0, 1, 255})
		ws.SetReadDeadline(time.Now().Add(time.Second))
		kind, data, err := ws.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(data, []byte{0, 1, 255}) {
			t.Fatalf("echo %v %v", data, err)
		}
		if i == 0 {
			ws.Close()
			continue
		}
		ws.SetReadDeadline(expires.Add(2 * time.Second))
		_, _, err = ws.ReadMessage()
		if err == nil || time.Now().Before(expires) {
			t.Fatalf("stream not closed at expiry: %v", err)
		}
		ws.Close()
	}
	if ws, res, err := dial.Dial(endpoint, nil); err == nil {
		ws.Close()
		t.Fatal("expired reconnect succeeded")
	} else if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
	peersMu.Lock()
	if len(forces) != 2 || !forces[0] || !forces[1] {
		t.Fatal(forces)
	}
	peersMu.Unlock()
	// A signed console token is not a resource credential and does not authorize REST.
	r, _ := http.NewRequest("GET", srv.URL+"/api/v1/hosts", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("token authorized API")
	}
}

func TestConsoleRejectsOriginAndMalformedUpgradeBeforeBackend(t *testing.T) {
	e := &Environ{cfg: &config.APIConfig{}}
	// A nil backend would panic if it were touched before validation.
	for _, origin := range []string{"https://evil.test", ""} {
		r := httptest.NewRequest("GET", "https://vmango.test/ws?force=true", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		e.serveConsole(w, r, "n", "v", "serial", true, time.Time{})
		want := 400
		if origin != "" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
}

func (r *apiVMRepo) ValidateNew(*compute.VirtualMachine) error { return r.getErr }

func TestBridgeRejectsTextAndClosesOnBackendEOF(t *testing.T) {
	for _, mode := range []string{"text", "eof", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			done := make(chan struct{})
			var backend net.Conn
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				up := websocket.Upgrader{}
				ws, err := up.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				a, b := net.Pipe()
				backend = b
				if mode == "eof" {
					b.Close()
				}
				bridgeConsole(ws, a, time.Time{})
				close(done)
			}))
			defer srv.Close()
			ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if mode == "text" {
				ws.WriteMessage(websocket.TextMessage, []byte("not binary"))
			}
			if mode == "oversized" {
				ws.WriteMessage(websocket.BinaryMessage, make([]byte, (1<<20)+1))
			}
			ws.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _, err = ws.ReadMessage()
			if err == nil {
				t.Fatal("connection was not closed")
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("bridge leaked a reader")
			}
			if backend != nil {
				backend.Close()
			}
		})
	}
}

func TestAPIValidationDoesNotEchoCloudInit(t *testing.T) {
	h := newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	w := apiRequest(h, "POST", "/api/v1/clouds/local/machines", `{"user_data":"private-bootstrap-secret"}`, true)
	if w.Code != 422 || strings.Contains(w.Body.String(), "private-bootstrap-secret") {
		t.Fatalf("validation echoed input: %d %s", w.Code, w.Body)
	}
}

const testJWTKey = "e702acd09d76676cd386642a315038470691f711b3821e1552bfffd37cb15b983"

func testWebToken() string {
	now := time.Now()
	p := auth.Principal{Method: "local", LocalID: "admin"}
	c := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: auth.Issuer, Audience: jwt.ClaimStrings{auth.Audience}, Subject: p.ID(), ID: strings.Repeat("a", 64), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(auth.AccessLifetime))}, Kind: "web", Purpose: "access", Principal: p}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(testJWTKey))
	if err != nil {
		panic(err)
	}
	return token
}
func testAuthService(t *testing.T, cfg *config.Config) *auth.Service {
	t.Helper()
	dir := t.TempDir()
	sessions, err := filesystem.NewAuthRepository(filepath.Join(dir, "sessions"), "refresh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sessions.Close() })
	tokens, err := filesystem.NewAuthRepository(filepath.Join(dir, "tokens"), "personal")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tokens.Close() })
	service, err := auth.New(cfg.API.JWTSigningKey, sessions, tokens, cfg.API.AuthPolicy().Eligible)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func newTestAPI(t *testing.T, cfg *config.Config, logger zerolog.Logger, networks *compute.NetworkService, keys *compute.KeyService, pools *compute.VolumePoolService, nodes *compute.NodeService, volumes *compute.VolumeService, vms *compute.VirtualMachineService, manager *compute.VirtualMachineManager, images *compute.ImageService) http.Handler {
	t.Helper()
	if manager == nil {
		manager = &compute.VirtualMachineManager{}
	}
	if manager.Clouds == nil {
		manager.Clouds = &compute.CloudService{Definitions: []compute.Cloud{{ID: "local", Hosts: []compute.CloudHost{{Node: "n"}, {Node: "local"}, {Node: "a"}, {Node: "b"}}}}}
	}
	h, err := New(cfg, logger, networks, keys, pools, nodes, volumes, vms, manager, images, testAuthService(t, cfg), newTestTaskService(t, &compute.CommandHandler{Manager: manager, VMs: vms, Volumes: volumes, Nodes: nodes, Keys: keys}))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func newTestTaskService(t *testing.T, handler tasks.Handler) *tasks.Service {
	t.Helper()
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	service := tasks.New(repo, handler, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	if err := service.Start(ctx, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); service.Wait(); repo.Close() })
	return service
}
func awaitAPITask(t *testing.T, h http.Handler, accepted *httptest.ResponseRecorder) tasks.Task {
	t.Helper()
	var ref apiTaskReference
	if err := json.Unmarshal(accepted.Body.Bytes(), &ref); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response := apiRequest(h, "GET", ref.StatusURL, "", true)
		var task tasks.Task
		if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.State.Terminal() {
			if task.State != tasks.Succeeded {
				t.Fatalf("task failed: %+v", task)
			}
			return task
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not finish")
	return tasks.Task{}
}
