package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestBrowserCredentialLifecycle(t *testing.T) {
	cfg := apiTestConfig(t)
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	login := apiRequest(h, "POST", "/api/v1/auth/login", `{"username":"admin","password":"secret"}`, false)
	if login.Code != 200 {
		t.Fatalf("%d %s", login.Code, login.Body)
	}
	var credentials struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &credentials); err != nil {
		t.Fatal(err)
	}
	if credentials.AccessToken == "" || credentials.TokenType != "Bearer" || credentials.ExpiresIn != 600 {
		t.Fatal("invalid access-token response", login.Body)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing refresh cookie", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != refreshCookie || cookie.Path != "/api/v1/auth/" || !cookie.HttpOnly || !cookie.Secure || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge > 7*86400 || cookie.MaxAge < 7*86400-10 {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	call := func(path string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://vmango.test"+path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	rotated := call("/api/v1/auth/refresh", cookie)
	if rotated.Code != 200 {
		t.Fatalf("refresh %d %s", rotated.Code, rotated.Body)
	}
	replacement := rotated.Result().Cookies()[0]
	if replacement.Value == cookie.Value || replacement.Expires.After(cookie.Expires) {
		t.Fatal("refresh did not rotate with an absolute lifetime")
	}
	if w := call("/api/v1/auth/refresh", cookie); w.Code != 401 {
		t.Fatal("lost response must require login", w.Code, w.Body)
	}
	if w := call("/api/v1/auth/refresh", replacement); w.Code != 401 {
		t.Fatal("reuse did not revoke family", w.Code, w.Body)
	}
	if w := call("/api/v1/auth/logout", cookie); w.Code != 204 {
		t.Fatal("logout after reuse", w.Code, w.Body)
	}
	if w := call("/api/v1/auth/logout", nil); w.Code != 204 {
		t.Fatal("logout without cookie must be safe", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "https://vmango.test/api/v1/me", nil)
	r.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("logout incorrectly revoked stateless web JWT", w.Code, w.Body)
	}
}

func TestAuthOriginsAndProxyTLS(t *testing.T) {
	for _, tc := range []struct {
		name, scheme, host, peer, base, origin, site string
		secure                                       bool
		want                                         int
	}{
		{"TLS", "https", "vmango.test", "192.0.2.1:42", "", "https://vmango.test", "same-origin", false, 200},
		{"proxy", "http", "vmango.test", "127.0.0.1:42", "https://vmango.test", "https://vmango.test", "same-origin", false, 200},
		{"native loopback", "http", "localhost", "127.0.0.1:42", "", "", "", false, 200},
		{"insecure remote", "http", "vmango.test", "192.0.2.1:42", "", "http://vmango.test", "same-origin", false, 403},
		{"cross origin", "https", "vmango.test", "192.0.2.1:42", "", "https://evil.test", "", false, 403},
		{"cross site", "https", "vmango.test", "192.0.2.1:42", "", "", "cross-site", false, 403},
		{"subdomain", "https", "vmango.test", "192.0.2.1:42", "", "https://other.vmango.test", "same-site", false, 403},
		{"downgrade", "http", "vmango.test", "127.0.0.1:42", "https://vmango.test", "http://vmango.test", "", false, 403},
		{"secure proxy flag", "http", "vmango.test", "127.0.0.1:42", "", "https://vmango.test", "", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := apiTestConfig(t)
			cfg.API.BaseUrl = tc.base
			cfg.API.SessionSecure = tc.secure
			h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
			r := httptest.NewRequest("POST", tc.scheme+"://"+tc.host+"/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
			r.RemoteAddr = tc.peer
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.want == 200 {
				cookies := w.Result().Cookies()
				wantSecure := tc.scheme == "https" || tc.base != "" || tc.secure
				if len(cookies) != 1 || cookies[0].Secure != wantSecure {
					t.Fatal("wrong TLS cookie", cookies)
				}
			}
		})
	}
}

func TestFrontendRoutingAndRuntimeAssets(t *testing.T) {
	h := newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	for _, path := range []string{"/machines/", "/tasks/", "/tasks/22d9e017-9379-4f6f-95ab-9823dbd1cc35/", "/machines/node/vm/update/", "/console/local/v/vnc", "/api/docs", "/vendor/novnc.html", "/vendor/vnc-client.js", "/vendor/noVNC-1.7.0/app/ui.js", "/vendor/noVNC-1.7.0/package.json", "/vendor/swagger-ui-5.34.0/swagger-ui-bundle.js"} {
		w := apiRequest(h, "GET", path, "", false)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/assets/missing.js", "/vendor/missing.css", "/unknown", "/console/local/v/serial/ws/", "/api/missing"} {
		w := apiRequest(h, "GET", path, "", true)
		if w.Code != 404 || strings.Contains(w.Body.String(), "<html") {
			t.Fatalf("fallback swallowed %s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := apiRequest(h, "POST", "/machines/v/delete/", "", false); w.Code != 405 {
		t.Fatal("legacy HTML mutation route survived", w.Code)
	}
	if w := apiRequest(h, "POST", "/api/v1/administration/clouds", "", true); w.Code != 405 || !strings.Contains(w.Header().Get("Content-Type"), "problem+json") {
		t.Fatal("API method error is not JSON", w.Code)
	}
	for _, path := range []string{"/machines/", "/api/docs"} {
		w := apiRequest(h, "HEAD", path, "", false)
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Fatal("HEAD shell", path, w.Code)
		}
	}
	vnc := apiRequest(h, "GET", "/vendor/vnc-client.js", "", false).Body.String()
	for _, required := range []string{"mandatory:", "host: ''", "port: ''", "vmango.token.", "event.origin !== location.origin", "encodeURIComponent(data.cloud)"} {
		if !strings.Contains(vnc, required) {
			t.Fatal("VNC connection pinning missing", required)
		}
	}
	page := apiRequest(h, "GET", "/vendor/novnc.html", "", false).Body.String()
	if strings.Contains(page, "{{") || !strings.Contains(page, "noVNC_clipboard") || !strings.Contains(page, "noVNC_send_ctrl_alt_del_button") {
		t.Fatal("full noVNC controls were lost")
	}
}

func TestLocalReturnPath(t *testing.T) {
	for _, raw := range []string{"https://evil.test/", "//evil.test/", "/\\evil.test/", "/%2f%2fevil.test/", "javascript:alert(1)"} {
		if localReturnPath(raw) != "/" {
			t.Fatal("unsafe OIDC redirect", raw)
		}
	}
	if localReturnPath("/machines/?node=n") != "/machines/?node=n" {
		t.Fatal("local destination lost")
	}
}
