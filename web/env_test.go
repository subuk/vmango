package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"subuk/vmango/compute"
	"subuk/vmango/config"

	"github.com/gorilla/sessions"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginCSRF(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, requestURL, baseURL, origin, fetchSite string
		secure                                       bool
		want                                         int
	}{
		{"http", "http://vmango.test/login/", "", "http://vmango.test", "", false, http.StatusFound},
		{"https", "https://vmango.test/login/", "", "https://vmango.test", "same-origin", false, http.StatusFound},
		{"https proxy", "http://vmango.test/login/", "https://vmango.test", "https://vmango.test", "same-origin", false, http.StatusFound},
		{"secure session proxy", "http://vmango.test/login/", "", "https://vmango.test", "", true, http.StatusFound},
		{"cross origin", "http://vmango.test/login/", "", "http://other.test", "", false, http.StatusForbidden},
		{"cross site", "https://vmango.test/login/", "", "https://other.test", "cross-site", false, http.StatusForbidden},
		{"same site subdomain", "https://vmango.test/login/", "", "https://other.vmango.test", "same-site", false, http.StatusForbidden},
		{"cross site without origin", "https://vmango.test/login/", "", "", "cross-site", false, http.StatusForbidden},
		{"metadata without origin", "https://vmango.test/login/", "", "", "same-origin", false, http.StatusFound},
		{"null origin", "http://vmango.test/login/", "", "null", "", false, http.StatusForbidden},
		{"different port", "http://vmango.test/login/", "", "http://vmango.test:9000", "", false, http.StatusForbidden},
		{"non browser", "http://vmango.test/login/", "", "", "", false, http.StatusFound},
		{"proxy downgrade", "http://vmango.test/login/", "https://vmango.test", "http://vmango.test", "", false, http.StatusForbidden},
		{"tls downgrade", "https://vmango.test/login/", "", "http://vmango.test", "", false, http.StatusForbidden},
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
			form := url.Values{"Username": {"admin"}, "Password": {"password"}}
			req := httptest.NewRequest(http.MethodPost, tt.requestURL, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("Sec-Fetch-Site", tt.fetchSite)
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

type vncTestRepository struct {
	compute.VirtualMachineRepository
}

func TestOriginlessRequestStillRequiresAuthentication(t *testing.T) {
	cfg := config.Default()
	cfg.Web.SessionSecret = "0123456789abcdef0123456789abcdef"
	handler := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/machines/node/vm/delete/", nil))
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/login/" {
		t.Fatalf("unauthenticated request: status %d, location %q", response.Code, response.Header().Get("Location"))
	}
}

func (vncTestRepository) Get(id, node string) (*compute.VirtualMachine, error) {
	return &compute.VirtualMachine{Id: id, NodeId: node}, nil
}

func TestVNCPageAndAssets(t *testing.T) {
	cfg := config.Default()
	cfg.Web.SessionSecret = "0123456789abcdef0123456789abcdef"
	store := sessions.NewCookieStore([]byte(cfg.Web.SessionSecret))
	req := httptest.NewRequest(http.MethodGet, "/machines/node/vm/vnc/", nil)
	session, err := store.New(req, SESSION_NAME)
	if err != nil {
		t.Fatal(err)
	}
	session.Values[SESSION_USER_KEY] = &User{Id: "test", Authenticated: true}
	cookies := httptest.NewRecorder()
	if err := session.Save(req, cookies); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies.Result().Cookies() {
		req.AddCookie(cookie)
	}
	handler := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil,
		compute.NewVirtualMachineService(vncTestRepository{}), nil)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, req)
	if page.Code != http.StatusOK {
		t.Fatalf("console returned %d: %s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), `path: "/machines/node/vm/vnc/ws/"`) {
		t.Fatal("console did not render the machine's WebSocket endpoint as a JS string")
	}
	assets := regexp.MustCompile(`(?:href=|src=|from )"(/static/[^"\s]+)"`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(assets) == 0 {
		t.Fatal("console has no assets")
	}
	for _, asset := range assets {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, asset[1], nil))
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Errorf("console asset %s: status %d, bytes %d", asset[1], response.Code, response.Body.Len())
		}
	}
}
