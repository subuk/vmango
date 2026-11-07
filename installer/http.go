package installer

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"subuk/vmango/ui"
)

// Handler is bound to one listener authority and never installed in normal web mode.
type Handler struct {
	controller                        *Controller
	authority, token, session, cookie string
	files                             http.Handler
	origins                           *http.CrossOriginProtection
}

func NewHandler(c *Controller, authority string) (*Handler, error) {
	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("installer must listen on a loopback IP")
	}
	return &Handler{controller: c, authority: authority, token: randomID(), session: randomID(), cookie: "vmango_install_" + randomID()[:12], files: http.FileServer(http.FS(ui.Files())), origins: http.NewCrossOriginProtection()}, nil
}
func (h *Handler) SessionURL() string { return "http://" + h.authority + "/install/#token=" + h.token }
func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, e error) {
	jsonReply(w, status, map[string]string{"error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("request must contain JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid request JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fmt.Errorf("invalid trailing request data")
	}
	return nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Host != h.authority {
		failure(w, 403, fmt.Errorf("unexpected installer host"))
		return
	}
	if err := h.origins.Check(r); err != nil {
		failure(w, 403, fmt.Errorf("cross-origin request rejected"))
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		origin := r.Header.Get("Origin")
		if origin != "http://"+h.authority {
			failure(w, 403, fmt.Errorf("installer requests require the same origin"))
			return
		}
	}
	if r.URL.Path == "/install/api/session" {
		if r.Method != "POST" {
			failure(w, 405, fmt.Errorf("POST required"))
			return
		}
		var input struct {
			Token string `json:"token"`
		}
		if err := decode(w, r, &input); err != nil {
			failure(w, 400, err)
			return
		}
		if subtle.ConstantTimeCompare([]byte(input.Token), []byte(h.token)) != 1 {
			failure(w, 401, fmt.Errorf("invalid installer session token"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: h.cookie, Value: h.session, Path: "/install/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		jsonReply(w, 200, map[string]bool{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/install/api/") {
		cookie, err := r.Cookie(h.cookie)
		if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(h.session)) != 1 {
			failure(w, 401, fmt.Errorf("open the session URL printed by vmango install"))
			return
		}
		if r.URL.Path == "/install/api/state" && r.Method == "GET" {
			jsonReply(w, 200, h.controller.Snapshot())
			return
		}
		if r.Method != "POST" {
			failure(w, 405, fmt.Errorf("POST required"))
			return
		}
		var action struct {
			Revision uint64 `json:"revision"`
			Stage    string `json:"stage,omitempty"`
			PlanID   string `json:"planID,omitempty"`
		}
		if r.URL.Path == "/install/api/answers" {
			var a Answers
			if err := decode(w, r, &a); err != nil {
				failure(w, 400, err)
				return
			}
			err = h.controller.Answers(a)
		} else {
			if err := decode(w, r, &action); err != nil {
				failure(w, 400, err)
				return
			}
			switch r.URL.Path {
			case "/install/api/inspect":
				err = h.controller.Inspect(action.Revision)
			case "/install/api/discover":
				err = h.controller.Discover(action.Revision)
			case "/install/api/prepare":
				err = h.controller.Prepare(action.Revision, action.Stage)
			case "/install/api/apply":
				err = h.controller.Apply(action.Revision, action.PlanID)
			case "/install/api/cancel":
				err = h.controller.Cancel()
			case "/install/api/resume":
				err = h.controller.Resume(action.Revision)
			default:
				failure(w, 404, fmt.Errorf("installer route not found"))
				return
			}
		}
		if err != nil {
			failure(w, 409, err)
			return
		}
		jsonReply(w, 200, h.controller.Snapshot())
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		failure(w, 405, fmt.Errorf("GET required"))
		return
	}
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/install/", http.StatusTemporaryRedirect)
		return
	}
	if r.URL.Path == "/install" || strings.HasPrefix(r.URL.Path, "/install/") {
		b, err := fs.ReadFile(ui.Files(), "install.html")
		if err != nil {
			failure(w, 503, fmt.Errorf("installer frontend is missing; rebuild embedded assets"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			w.Write(b)
		}
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/theme-init.js" || r.URL.Path == "/favicon.ico" {
		h.files.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}
