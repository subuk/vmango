package api

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestAdministrationDeepLinksAndRetiredHostPages(t *testing.T) {
	h := newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	for _, path := range []string{
		"/administration/",
		"/administration/configuration/",
		"/administration/clouds/",
		"/administration/clouds/cloud.one/",
		"/administration/clouds/cloud.one/configuration/",
		"/administration/clouds/cloud.one/configuration/hosts/",
		"/administration/clouds/cloud.one/configuration/hosts/host.one/",
		"/administration/clouds/cloud.one/hosts/",
		"/administration/clouds/cloud.one/hosts/host.one/",
		"/administration/clouds/cloud.one/images/",
		"/administration/clouds/cloud.one/images/image.one/",
		"/tasks/",
		"/tasks/task.one/",
	} {
		for _, method := range []string{"GET", "HEAD"} {
			w := apiRequest(h, method, path, "", false)
			if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("deep link %s %s: %d %s", method, path, w.Code, w.Body)
			}
			if method == "GET" && !strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>") {
				t.Fatalf("deep link served no application shell: %s", path)
			}
			if method == "HEAD" && w.Body.Len() != 0 {
				t.Fatalf("HEAD deep link returned a body: %s", path)
			}
		}
	}
	for _, path := range []string{"/nodes/", "/nodes/host.one/", "/assets/missing.js", "/vendor/missing.js", "/api/v1/missing"} {
		w := apiRequest(h, "GET", path, "", true)
		if w.Code != 404 || strings.Contains(w.Body.String(), "<!doctype html>") || w.Header().Get("Location") != "" {
			t.Fatalf("removed/missing path served a shell or redirect: %s %d %s", path, w.Code, w.Body)
		}
	}
}
