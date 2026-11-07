package installer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"subuk/vmango/compute"
	"subuk/vmango/installer/richt"
)

const testAuthority = "127.0.0.1:4444"

func testHandler(t *testing.T, p *fakeProvisioner) (*Handler, *Controller) {
	t.Helper()
	c, _ := newTestController(t, true, p, nil)
	h, err := NewHandler(c, testAuthority)
	if err != nil {
		t.Fatal(err)
	}
	return h, c
}

func TestInstallerHTTPDiscoverRequiresSessionAndCurrentRevision(t *testing.T) {
	r := &observingResources{fakeResources: &fakeResources{}, observe: func(context.Context, string, compute.HostPreparationSpec) (compute.HostPreparationState, error) {
		return existingTestResources(), nil
	}}
	h, c := testHandler(t, nil)
	c.opts.Resources = func([]Host) Resources { return r }
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	revision := c.Snapshot().Revision
	body, _ := json.Marshal(map[string]uint64{"revision": revision})
	cookie := sessionCookie(t, h)
	for _, tc := range []struct {
		name, body, origin string
		cookie             *http.Cookie
		status             int
	}{
		{"missing session", string(body), "http://" + testAuthority, nil, 401},
		{"wrong session nonce", string(body), "http://" + testAuthority, &http.Cookie{Name: cookie.Name, Value: "wrong"}, 401},
		{"foreign origin", string(body), "https://example.org", cookie, 403},
		{"missing revision", `{}`, "http://" + testAuthority, cookie, 409},
		{"stale revision", `{"revision":0}`, "http://" + testAuthority, cookie, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := handlerRequest(h, "POST", "/install/api/discover", tc.body, tc.origin, tc.cookie)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if r.observeCalls.Load() != 0 || c.Snapshot().Busy {
				t.Fatal("rejected discovery launched work")
			}
		})
	}
	w := handlerRequest(h, "POST", "/install/api/discover", string(body), "http://"+testAuthority, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s := waitInstaller(t, c)
	if r.observeCalls.Load() != 1 || s.Hosts[0].Subnet != "10.48.3.0/24" || s.Revision != revision+1 {
		t.Fatalf("route did not load existing settings: %+v", s)
	}
	if r.prepareCalls.Load() != 0 || r.applyCalls.Load() != 0 {
		t.Fatal("discovery route mutated resources")
	}
	w = handlerRequest(h, "POST", "/install/api/discover", string(body), "http://"+testAuthority, cookie)
	if w.Code != 409 || r.observeCalls.Load() != 1 {
		t.Fatal("accepted old revision after discovery", w.Code)
	}
}
func handlerRequest(h *Handler, method, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+testAuthority+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func sessionCookie(t *testing.T, h *Handler) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": h.token})
	w := handlerRequest(h, "POST", "/install/api/session", string(body), "http://"+testAuthority, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal(cookies)
	}
	return cookies[0]
}

func TestInstallerHTTPSessionRequiresTokenAndScopesCookie(t *testing.T) {
	h, _ := testHandler(t, nil)
	session, err := url.Parse(h.SessionURL())
	if err != nil {
		t.Fatal(err)
	}
	if session.RawQuery != "" || !strings.HasPrefix(session.Fragment, "token=") {
		t.Fatal("session token is not confined to URL fragment")
	}
	for _, body := range []string{`{}`, `{"token":"wrong"}`} {
		w := handlerRequest(h, "POST", "/install/api/session", body, "http://"+testAuthority, nil)
		if w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	cookie := sessionCookie(t, h)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/install/" || cookie.Value == h.token {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	w := handlerRequest(h, "GET", "/install/api/state", "", "", nil)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	w = handlerRequest(h, "GET", "/install/api/state", "", "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing private-response headers")
	}
	if strings.Contains(w.Body.String(), h.token) || strings.Contains(w.Body.String(), h.session) || strings.Contains(w.Body.String(), "existing-private-hash") {
		t.Fatal("state leaks credentials")
	}
}

func TestInstallerHTTPRejectsForeignHostOriginAndCrossSiteRequests(t *testing.T) {
	h, c := testHandler(t, nil)
	cookie := sessionCookie(t, h)
	for _, tc := range []struct{ name, host, origin, site string }{
		{"foreign host", "evil.example", "http://" + testAuthority, "same-origin"},
		{"foreign origin", testAuthority, "https://evil.example", ""},
		{"missing origin", testAuthority, "", ""},
		{"cross site", testAuthority, "http://" + testAuthority, "cross-site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+testAuthority+"/install/api/inspect", strings.NewReader(`{"revision":1}`))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatal(w.Code, w.Body.String())
			}
			if c.Snapshot().Busy {
				t.Fatal("rejected request launched work")
			}
		})
	}
	if _, err := NewHandler(c, "0.0.0.0:4444"); err == nil {
		t.Fatal("installer accepted a public listener")
	}
}

func TestInstallerHTTPRejectsMalformedMutationBodies(t *testing.T) {
	h, _ := testHandler(t, nil)
	cookie := sessionCookie(t, h)
	for _, body := range []string{`{"revision":1,"configPath":"/etc/other.conf"}`, `{"revision":1} {}`, `not-json`, strings.Repeat("x", 129<<10)} {
		w := handlerRequest(h, "POST", "/install/api/inspect", body, "http://"+testAuthority, cookie)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := handlerRequest(h, "GET", "/install/api/apply", "", "", cookie)
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
}

func TestInstallerHTTPPlanAndExecutionOutputRemainEscapedData(t *testing.T) {
	p := &fakeProvisioner{prepare: func(_ context.Context, _ []richt.Target, log func(string)) (*richt.Plan, error) {
		log(`<script>danger()</script>`)
		return &richt.Plan{ID: "private-id", Path: "/private/raw-plan.json", Digest: "private-digest", Summary: `<img src=x onerror="danger()">`}, nil
	}}
	h, c := testHandler(t, p)
	cookie := sessionCookie(t, h)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	prepareStage(t, c, "hosts")
	w := handlerRequest(h, "GET", "/install/api/state", "", "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatal("output was not JSON")
	}
	if strings.Contains(w.Body.String(), "<script>") || strings.Contains(w.Body.String(), "<img") || strings.Contains(w.Body.String(), "/private/raw-plan.json") || strings.Contains(w.Body.String(), "private-digest") {
		t.Fatal("executable markup or private plan was exposed")
	}
	var state State
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.Plan.Output, "<img") || !strings.Contains(strings.Join(state.Logs, ""), "<script>") {
		t.Fatal("safe text output was unexpectedly lost")
	}
	w = handlerRequest(h, "GET", "/install/reconnect", "", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<title>Set up Vmango</title>") {
		t.Fatal("installer deep link did not serve its own entry", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("missing frame protection")
	}
}

func TestInstallerHTTPBrowserDisconnectDoesNotCancelApprovedWork(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	p := &fakeProvisioner{apply: func(ctx context.Context, _ *richt.Plan, _ func(string)) error {
		started <- ctx
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	h, c := testHandler(t, p)
	cookie := sessionCookie(t, h)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	s := prepareStage(t, c, "hosts")
	body, _ := json.Marshal(map[string]any{"revision": s.Revision, "planID": s.Plan.ID})
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "http://"+testAuthority+"/install/api/apply", strings.NewReader(string(body))).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://"+testAuthority)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	job := <-started
	cancel()
	select {
	case <-job.Done():
		t.Fatal("browser request cancellation stopped approved work")
	case <-time.After(20 * time.Millisecond):
	}
	w = handlerRequest(h, "GET", "/install/api/state", "", "", cookie)
	var current State
	if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if !current.Busy {
		t.Fatal("reconnected browser did not observe active job")
	}
	close(release)
	if s = waitInstaller(t, c); s.Error != "" {
		t.Fatal(s.Error)
	}
}
