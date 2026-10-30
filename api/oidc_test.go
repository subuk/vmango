package api

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
	"subuk/vmango/config"
)

func oidcTestEnv() *Environ {
	cfg := config.Default()
	return &Environ{cfg: &cfg.API, logger: zerolog.Nop(), oidcp: &oidc.Provider{}, oauth2: &oauth2.Config{
		ClientID: "client", ClientSecret: "secret", RedirectURL: "https://vmango.test/callback",
		Endpoint: oauth2.Endpoint{AuthURL: "https://provider.test/auth", AuthStyle: oauth2.AuthStyleInParams},
	}}
}

func TestOidcPKCECookies(t *testing.T) {
	for _, tt := range []struct {
		name, requestURL, baseURL string
		sessionSecure, secure     bool
	}{
		{"http", "http://vmango.test/login", "", false, false},
		{"tls", "https://vmango.test/login", "", false, true},
		{"proxy base URL", "http://vmango.test/login", "https://vmango.test", false, true},
		{"proxy session setting", "http://vmango.test/login", "", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := oidcTestEnv()
			env.cfg.BaseUrl, env.cfg.SessionSecure = tt.baseURL, tt.sessionSecure
			var previousState, previousVerifier string
			for i := 0; i < 2; i++ {
				w := httptest.NewRecorder()
				env.OidcLoginRedirect(w, httptest.NewRequest("GET", tt.requestURL, nil))
				if w.Code != http.StatusFound {
					t.Fatalf("status = %d", w.Code)
				}
				u, err := url.Parse(w.Header().Get("Location"))
				if err != nil {
					t.Fatal(err)
				}
				cookies := map[string]*http.Cookie{}
				for _, c := range w.Result().Cookies() {
					cookies[c.Name] = c
				}
				for _, name := range []string{"oauthstate", "oauthverifier"} {
					c := cookies[name]
					if c == nil {
						t.Fatalf("missing %s", name)
					}
					if !c.HttpOnly || c.Secure != tt.secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 900 || c.Expires.IsZero() {
						t.Fatalf("unexpected cookie: %+v", c)
					}
				}
				state, verifier := cookies["oauthstate"].Value, cookies["oauthverifier"].Value
				if state == "" || state == previousState || verifier == previousVerifier || len(verifier) < 43 {
					t.Fatal("missing or reused credentials")
				}
				hash := sha256.Sum256([]byte(verifier))
				if u.Query().Get("state") != state || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
					t.Fatalf("invalid authorization parameters: %v", u.Query())
				}
				previousState, previousVerifier = state, verifier
			}
		})
	}
}

func TestOidcCallbackPKCE(t *testing.T) {
	for _, tt := range []struct {
		name, state, cookieState, verifier string
		omitState, omitVerifier, exchange  bool
	}{
		{name: "valid", state: "state", cookieState: "state", verifier: "verifier", exchange: true},
		{name: "wrong state", state: "wrong", cookieState: "state", verifier: "verifier"},
		{name: "missing state cookie", state: "state", omitState: true, verifier: "verifier"},
		{name: "empty state", verifier: "verifier"},
		{name: "missing verifier", state: "state", cookieState: "state", omitVerifier: true},
		{name: "empty verifier", state: "state", cookieState: "state"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("code_verifier") != tt.verifier || r.Form.Get("code") != "auth-code" || r.Form.Get("grant_type") != "authorization_code" {
					t.Errorf("unexpected exchange: %v", r.Form)
				}
				w.Header().Set("Content-Type", "application/json")
				// Stop before ID-token validation: this test covers the authorization-code exchange.
				_, _ = w.Write([]byte(`{"access_token":"token","token_type":"Bearer"}`))
			}))
			defer server.Close()
			env := oidcTestEnv()
			env.oauth2.Endpoint.TokenURL = server.URL
			r := httptest.NewRequest("GET", "https://vmango.test/callback?code=auth-code&state="+url.QueryEscape(tt.state), nil)
			if !tt.omitState {
				r.AddCookie(&http.Cookie{Name: "oauthstate", Value: tt.cookieState})
			}
			if !tt.omitVerifier {
				r.AddCookie(&http.Cookie{Name: "oauthverifier", Value: tt.verifier})
			}
			w := httptest.NewRecorder()
			env.OidcLoginCallback(w, r)
			wantCalls, wantStatus := 0, http.StatusBadRequest
			if tt.exchange {
				wantCalls, wantStatus = 1, http.StatusUnauthorized
			}
			if calls != wantCalls || w.Code != wantStatus {
				t.Fatalf("calls=%d status=%d body=%s", calls, w.Code, w.Body.String())
			}
			if tt.state != "" && tt.state == tt.cookieState && !tt.omitState {
				cleared := map[string]bool{}
				for _, c := range w.Result().Cookies() {
					if c.MaxAge != -1 || c.Value != "" || !c.Secure || !c.HttpOnly || c.Path != "/" {
						t.Errorf("invalid deletion: %+v", c)
					}
					cleared[c.Name] = true
				}
				if !cleared["oauthstate"] || !cleared["oauthverifier"] {
					t.Fatal("temporary cookies were not cleared")
				}
			}
		})
	}
}
