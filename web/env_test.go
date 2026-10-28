package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"subuk/vmango/config"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginCSRF(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, requestURL, baseURL, origin string
		secure, omitToken                 bool
		want                              int
	}{
		{"http", "http://vmango.test/login/", "", "http://vmango.test", false, false, http.StatusFound},
		{"https", "https://vmango.test/login/", "", "https://vmango.test", false, false, http.StatusFound},
		{"https proxy", "http://vmango.test/login/", "https://vmango.test", "https://vmango.test", false, false, http.StatusFound},
		{"secure session proxy", "http://vmango.test/login/", "", "https://vmango.test", true, false, http.StatusFound},
		{"cross origin", "http://vmango.test/login/", "", "http://other.test", false, false, http.StatusBadRequest},
		{"missing token", "http://vmango.test/login/", "", "http://vmango.test", false, true, http.StatusBadRequest},
		{"proxy downgrade", "http://vmango.test/login/", "https://vmango.test", "http://vmango.test", false, false, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Web.SessionSecret = "0123456789abcdef0123456789abcdef"
			cfg.Web.BaseUrl = tt.baseURL
			cfg.Web.SessionSecure = tt.secure
			cfg.Web.Users = []config.UserWebConfig{{Id: "admin", HashedPassword: string(hash)}}
			handler := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil)
			get := httptest.NewRecorder()
			handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, tt.requestURL, nil))
			if get.Code != http.StatusOK {
				t.Fatalf("login page: %d: %s", get.Code, get.Body.String())
			}
			token := regexp.MustCompile(`name="csrf"[^>]*value="([^"]+)"`).FindStringSubmatch(get.Body.String())
			if len(token) != 2 {
				t.Fatal("login page has no CSRF token")
			}
			form := url.Values{"Username": {"admin"}, "Password": {"password"}}
			if !tt.omitToken {
				form.Set("csrf", token[1])
			}
			req := httptest.NewRequest(http.MethodPost, tt.requestURL, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", tt.origin)
			for _, cookie := range get.Result().Cookies() {
				req.AddCookie(cookie)
			}
			post := httptest.NewRecorder()
			handler.ServeHTTP(post, req)
			if post.Code != tt.want {
				t.Fatalf("login returned %d, want %d: %s", post.Code, tt.want, post.Body.String())
			}
			if tt.want == http.StatusFound {
				authenticated := false
				for _, cookie := range post.Result().Cookies() {
					if cookie.Name == SESSION_NAME {
						authenticated = true
					}
				}
				if !authenticated {
					t.Fatal("login did not create a session cookie")
				}
			}
		})
	}
}
