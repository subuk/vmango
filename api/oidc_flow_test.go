package api

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/rs/zerolog"
)

func TestOIDCCallbackRefreshAndVerifiedIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	verified := true
	issuer := ""
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("code_verifier") == "" {
				t.Error("PKCE verifier missing")
			}
			body, _ := json.Marshal(map[string]any{"iss": issuer, "sub": "stable-subject", "aud": "client", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "email": "allowed@example.com", "email_verified": verified, "name": "OIDC user"})
			signed, err := signer.Sign(body)
			if err != nil {
				t.Error(err)
				return
			}
			raw, err := signed.CompactSerialize()
			if err != nil {
				t.Error(err)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "provider-secret-not-persisted", "token_type": "Bearer", "id_token": raw})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	cfg := apiTestConfig(t)
	cfg.API.Oidc.IssuerUrl = issuer
	cfg.API.Oidc.ClientId = "client"
	cfg.API.Oidc.ClientSecret = "client-secret"
	cfg.API.Oidc.Scopes = []string{"openid", "profile", "email"}
	cfg.API.Oidc.AllowedEmails = []string{"allowed@example.com"}
	cfg.API.BaseUrl = "https://vmango.test"
	service := testAuthService(t, cfg)
	h, err := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil, service)
	if err != nil {
		t.Fatal(err)
	}
	login := apiRequest(h, "GET", "/login-oidc/?next="+url.QueryEscape("/machines/?node=local"), "", false)
	if login.Code != 302 {
		t.Fatal(login.Code, login.Body)
	}
	authURL, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callback := func(accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://vmango.test/login-oidc-callback/?code=code&state="+url.QueryEscape(authURL.Query().Get("state"))+"&next=https://evil.test", nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Accept", accept)
		for _, c := range login.Result().Cookies() {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	returned := callback("text/html")
	if returned.Code != 302 || returned.Header().Get("Location") != "/machines/?node=local" {
		t.Fatal("OIDC destination or cross-site callback failed", returned.Code, returned.Header(), returned.Body)
	}
	var refresh *http.Cookie
	for _, c := range returned.Result().Cookies() {
		if c.Name == refreshCookie {
			refresh = c
		}
	}
	if refresh == nil || !refresh.HttpOnly || !refresh.Secure {
		t.Fatal("OIDC did not persist a private refresh session")
	}
	r := httptest.NewRequest("POST", "https://vmango.test/api/v1/auth/refresh", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(refresh)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	c, err := service.Authenticate(body.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if c.Principal.Method != "oidc" || c.Principal.Issuer != issuer || c.Principal.Subject != "stable-subject" || !c.Principal.EmailVerified {
		t.Fatal("stable OIDC identity lost", c.Principal)
	}
	verified = false
	rejected := callback("application/json")
	if rejected.Code != 403 {
		t.Fatal("unverified allowlisted email accepted", rejected.Code, rejected.Body)
	}
	page := callback("text/html")
	if page.Code != 303 || page.Header().Get("Location") != "/login/?auth_error=credential_forbidden" {
		t.Fatal("browser OIDC error did not return to themed login", page.Code, page.Header())
	}
}
