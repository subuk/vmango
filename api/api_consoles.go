package api

import (
	"context"
	"encoding/base64"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net/http"
	"net/url"
	"strings"
	"subuk/vmango/auth"
	"subuk/vmango/compute"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type consoleClaim struct {
	Version int    `json:"v"`
	Purpose string `json:"purpose"`
	User    string `json:"user"`
	Node    string `json:"node"`
	VM      string `json:"vm"`
	Kind    string `json:"kind"`
	Force   bool   `json:"force,omitempty"`
	Expires int64  `json:"exp"`
}

func (e *Environ) consoleKey() ([]byte, error) {
	if len(e.cfg.ConsoleSigningKey) < 32 || e.cfg.ConsoleSigningKey == e.cfg.JWTSigningKey {
		return nil, compute.ErrUnavailable
	}
	return []byte(e.cfg.ConsoleSigningKey), nil
}
func (e *Environ) signConsole(c consoleClaim) (string, error) {
	key, err := e.consoleKey()
	if err != nil {
		return "", err
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"v": c.Version, "iss": auth.Issuer, "aud": "vmango-console", "sub": c.User, "purpose": c.Purpose, "node": c.Node, "vm": c.VM, "kind": c.Kind, "force": c.Force, "iat": time.Now().Unix(), "exp": c.Expires}).SignedString(key)
}
func (e *Environ) verifyConsole(raw, node, vm, kind string, now time.Time) (consoleClaim, error) {
	var c consoleClaim
	if len(raw) > 8192 {
		return c, auth.ErrInvalid
	}
	key, err := e.consoleKey()
	if err != nil {
		return c, err
	}
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(auth.Issuer), jwt.WithAudience("vmango-console"), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		return c, auth.ErrInvalid
	}
	m, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return c, auth.ErrInvalid
	}
	sub, _ := m.GetSubject()
	exp, _ := m.GetExpirationTime()
	if m["purpose"] != "console" || m["v"] != float64(1) || sub == "" || m["node"] != node || m["vm"] != vm || m["kind"] != kind || (kind != "serial" && kind != "vnc") || exp == nil {
		return c, auth.ErrInvalid
	}
	force, _ := m["force"].(bool)
	return consoleClaim{1, "console", sub, node, vm, kind, force, exp.Unix()}, nil
}

type apiConsoleLinkInput struct {
	Cloud string `path:"cloud"`
	Node  string `path:"node"`
	VM    string `path:"vm"`
	Kind  string `path:"kind" enum:"serial,vnc"`
	Body  struct {
		Force bool `json:"force,omitempty" doc:"Serial only: allow takeover on every connection using this link."`
	}
}
type apiConsoleLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (e *Environ) registerConsoles(api huma.API, router *mux.Router) {
	const path = "/clouds/{cloud}/machines/{node}/{vm}/consoles/{kind}"
	registerAPI(api, "POST", path+"/links", "create-console-link", "Issue reusable signed browser console link", func(ctx context.Context, in *apiConsoleLinkInput) (*apiResult[apiConsoleLink], error) {
		if claims(ctx).Kind != "web" {
			return nil, authError(auth.ErrForbidden)
		}
		vm, err := e.vms.Get(in.VM, in.Node)
		if err != nil {
			return nil, apiError(err)
		}
		if in.Kind == "vnc" && !vm.Graphic.Vnc() {
			return nil, apiError(compute.ErrConflict)
		}
		if in.Kind == "vnc" && in.Body.Force {
			return nil, apiError(compute.ErrInvalid)
		}
		ttl := e.cfg.ConsoleLinkLifetimeSeconds
		if ttl <= 0 {
			ttl = 3600
		}
		expires := time.Now().Add(time.Duration(ttl) * time.Second).Truncate(time.Second)
		token, err := e.signConsole(consoleClaim{1, "console", claims(ctx).Subject, in.Node, in.VM, in.Kind, in.Body.Force, expires.Unix()})
		if err != nil {
			return nil, apiError(err)
		}
		link := "/console/" + url.PathEscape(in.Node) + "/" + url.PathEscape(in.VM) + "/" + in.Kind + "#" + token
		if e.cfg.BaseUrl != "" {
			link = strings.TrimRight(e.cfg.BaseUrl, "/") + link
		}
		return &apiResult[apiConsoleLink]{Body: apiConsoleLink{link, expires}}, nil
	})
	native := "/api/v1" + path + "/ws"
	router.HandleFunc(native, func(w http.ResponseWriter, r *http.Request) {
		for key := range r.URL.Query() {
			if key != "force" {
				writeAPIProblem(w, 422, "Only the force query parameter is supported; credentials must never be placed in URLs.")
				return
			}
		}
		v := mux.Vars(r)
		force := r.URL.Query().Get("force")
		if force != "" && force != "false" && force != "true" {
			writeAPIProblem(w, 422, "force must be true or false.")
			return
		}
		e.serveConsole(w, r, v["node"], v["vm"], v["kind"], force == "true", claims(r.Context()).ExpiresAt.Time)
	}).Methods("GET")
	api.OpenAPI().AddOperation(&huma.Operation{Method: "GET", Path: native, OperationID: "connect-console", Tags: []string{"clouds"}, Summary: "Binary serial or VNC WebSocket; Bearer JWT on handshake", Description: consoleProtocol, Parameters: []*huma.Param{{Name: "cloud", In: "path", Required: true, Schema: &huma.Schema{Type: "string"}}, {Name: "node", In: "path", Required: true, Schema: &huma.Schema{Type: "string"}}, {Name: "vm", In: "path", Required: true, Schema: &huma.Schema{Type: "string"}}, {Name: "kind", In: "path", Required: true, Schema: &huma.Schema{Type: "string", Enum: []any{"serial", "vnc"}}}, {Name: "force", In: "query", Schema: &huma.Schema{Type: "boolean", Default: false}}}, Responses: map[string]*huma.Response{"101": {Description: "Switching protocols; binary messages only"}, "401": {Description: "Invalid Bearer credential"}, "403": {Description: "Origin or transport rejected"}, "404": {Description: "Missing resource"}, "409": {Description: "Console busy or VM state conflict"}, "503": {Description: "Backend unavailable"}}})
	router.HandleFunc("/api/console-protocol", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, consoleProtocol)
	}).Methods("GET")
	e.router.Handle("/console/{node}/{vm}/{kind}/ws", e.apiGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			writeAPIProblem(w, 422, "Console WebSocket credentials belong in the scoped subprotocol, never URL queries.")
			return
		}
		v := mux.Vars(r)
		token := ""
		for _, p := range websocket.Subprotocols(r) {
			if strings.HasPrefix(p, "vmango.token.") {
				if token != "" {
					writeAPIProblem(w, 401, "Invalid console link.")
					return
				}
				token = strings.TrimPrefix(p, "vmango.token.")
			}
		}
		c, err := e.verifyConsole(token, v["node"], v["vm"], v["kind"], time.Now())
		if err != nil {
			writeAPIProblem(w, 401, "Invalid or expired console link.")
			return
		}
		e.serveConsole(w, r, c.Node, c.VM, c.Kind, c.Force, time.Unix(c.Expires, 0))
	}), false)).Methods("GET")

}

func (e *Environ) serveConsole(w http.ResponseWriter, r *http.Request, node, id, kind string, force bool, expires time.Time) {
	if !e.apiOriginAllowed(r) {
		writeAPIProblem(w, 403, "Cross-origin requests are disabled.")
		return
	}
	key, keyErr := base64.StdEncoding.DecodeString(r.Header.Get("Sec-WebSocket-Key"))
	if !websocket.IsWebSocketUpgrade(r) || r.Header.Get("Sec-WebSocket-Version") != "13" || keyErr != nil || len(key) != 16 {
		writeAPIProblem(w, 400, "A valid WebSocket upgrade is required.")
		return
	}

	if kind != "serial" && kind != "vnc" {
		writeAPIProblem(w, 404, "Console not found.")
		return
	}
	if force && kind != "serial" {
		writeAPIProblem(w, 422, "force is supported only for serial consoles.")
		return
	}
	if !expires.IsZero() && !time.Now().Before(expires) {
		writeAPIProblem(w, 401, "Console link expired.")
		return
	}
	vm, err := e.vms.Get(id, node)
	if err == nil && (!vm.IsRunning() || kind == "vnc" && !vm.Graphic.Vnc()) {
		err = compute.ErrConflict
	}
	var stream io.ReadWriteCloser
	if err == nil {
		if kind == "serial" {
			stream, err = e.vms.GetConsoleStream(id, node, force)
		} else {
			stream, err = e.vms.GetGraphicStream(id, node)
		}
	}
	if err != nil {
		p := apiError(err)
		status := p.(interface{ GetStatus() int }).GetStatus()
		writeAPIProblem(w, status, http.StatusText(status))
		return
	}
	stream = &onceConsoleStream{ReadWriteCloser: stream}
	defer stream.Close()
	if !expires.IsZero() && !time.Now().Before(expires) {
		writeAPIProblem(w, 401, "Console link expired.")
		return
	}
	up := websocket.Upgrader{Subprotocols: []string{"binary"}, CheckOrigin: e.apiOriginAllowed, HandshakeTimeout: 10 * time.Second}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	if c := claims(r.Context()); c != nil {
		release, err := e.auth.RegisterStream(bearer(r), c, func() { ws.Close(); stream.Close() })
		if err != nil {
			ws.Close()
			return
		}
		defer release()
	}
	bridgeConsole(ws, stream, expires)
}

// Closing either transport must interrupt both readers. Only the backend pump
// writes data frames; control frames are safe to write concurrently.
func bridgeConsole(ws *websocket.Conn, stream io.ReadWriteCloser, expires time.Time) {
	var once sync.Once
	closeBoth := func() { once.Do(func() { ws.Close(); stream.Close() }) }
	defer closeBoth()
	ws.SetReadLimit(1 << 20)
	if !expires.IsZero() {
		ws.SetReadDeadline(expires)
	}
	var timer *time.Timer
	if !expires.IsZero() {
		timer = time.AfterFunc(time.Until(expires), func() {
			ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "console link expired"), time.Now().Add(time.Second))
			closeBoth()
		})
		defer timer.Stop()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer closeBoth()
		buf := make([]byte, 32<<10)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				deadline := time.Now().Add(15 * time.Second)
				if !expires.IsZero() && expires.Before(deadline) {
					deadline = expires
				}
				ws.SetWriteDeadline(deadline)
				if ws.WriteMessage(websocket.BinaryMessage, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		kind, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if kind != websocket.BinaryMessage {
			ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseUnsupportedData, "binary messages required"), time.Now().Add(time.Second))
			break
		}
		if _, err = io.Copy(stream, strings.NewReader(string(data))); err != nil {
			break
		}
	}
	closeBoth()
	<-done
}

const consoleProtocol = `Console protocol v1
Native clients: GET /api/v1/clouds/{cloud}/machines/{node}/{vm}/consoles/{serial|vnc}/ws
Send Authorization: Bearer TOKEN on the WebSocket handshake over WSS.
Access-token expiry closes the stream; reconnect with a valid token.
Revoking a personal token through the API closes its active streams.
The optional WebSocket subprotocol is binary. Each binary message carries raw
serial bytes or VNC/RFB bytes; message boundaries are not protocol boundaries.
Messages are limited to 1 MiB. Text messages close with code 1003. EOF or either
side disconnecting closes both transports. A slow WebSocket writer is closed
after 15 seconds. Errors before upgrade use application/problem+json.
Serial returns 409 when occupied; ?force=true explicitly takes over. VNC uses
shared-client mode in the browser; the backend controls shared-client support.
Browser links use /console/{node}/{vm}/{kind}#TOKEN. The public page is an empty
client shell. TOKEN is sent only as a vmango.token.TOKEN WebSocket subprotocol,
alongside binary; only binary is selected by the server. Never log that header.
The token authorizes only its bound console. It cannot access the API or UI.
Links are reusable until their absolute expiry; existing streams close then
(code 1008 when deliverable). Reconnection does not extend the expiry.
Key rotation rejects subsequent handshakes; it does not terminate active streams
before their original expiry. Individual link revocation is not supported.
`

type onceConsoleStream struct {
	io.ReadWriteCloser
	once sync.Once
}

func (s *onceConsoleStream) Close() error {
	var err error
	s.once.Do(func() { err = s.ReadWriteCloser.Close() })
	return err
}
