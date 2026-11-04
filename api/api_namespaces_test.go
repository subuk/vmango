package api

import (
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"net/http"
	"net/http/httptest"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestNamespaceHeadersAndAdministrativeIndependence(t *testing.T) {
	registry, err := compute.NewNamespaceService([]compute.Namespace{{ID: "platform", Clouds: []string{"local"}}, {ID: "analytics", Clouds: []string{"other"}}})
	if err != nil {
		t.Fatal(err)
	}
	env := &Environ{namespaces: registry, workloads: &compute.WorkloadService{}}
	for _, tt := range []struct {
		name, path, cloud string
		values            []string
		want              int
	}{
		{"missing", "/api/v1/clouds", "", nil, 400},
		{"empty", "/api/v1/clouds", "", []string{""}, 400},
		{"duplicate", "/api/v1/clouds", "", []string{"platform", "platform"}, 400},
		{"combined", "/api/v1/clouds", "", []string{"platform,analytics"}, 400},
		{"malformed", "/api/v1/clouds", "", []string{" platform"}, 400},
		{"unknown", "/api/v1/clouds", "", []string{"removed"}, 404},
		{"wrong cloud", "/api/v1/clouds/other/machines", "other", []string{"platform"}, 404},
		{"query selector", "/api/v1/clouds?namespace=analytics", "", []string{"platform"}, 400},
		{"valid", "/api/v1/clouds/local/machines", "local", []string{"platform"}, 204},
		{"administration", "/api/v1/administration/tasks", "", []string{"invalid", "other"}, 204},
		{"discovery", "/api/v1/namespaces", "", nil, 204},
		{"self", "/api/v1/me", "", nil, 204},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.path, nil)
			for _, value := range tt.values {
				r.Header.Add("X-Vmango-Namespace", value)
			}
			r.Header.Set("X-Vmango-Context", "cloud-admin")
			r = mux.SetURLVars(r, map[string]string{"cloud": tt.cloud})
			w := httptest.NewRecorder()
			env.namespaceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if workloadPath(r.URL.Path) && requestNamespace(r.Context()) != "platform" {
					t.Fatal("lost validated request context")
				}
				if !workloadPath(r.URL.Path) && requestNamespace(r.Context()) != "" {
					t.Fatal("administration inherited workspace")
				}
				w.WriteHeader(204)
			})).ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status %d, want %d: %s", w.Code, tt.want, w.Body)
			}
		})
	}
}
func TestNamespaceDiscoveryAndSchemaClassification(t *testing.T) {
	cfg := apiTestConfig(t)
	h := newTestAPI(t, cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil)
	for _, path := range []string{"/api/v1/namespaces", "/api/v1/administration/configuration/namespaces"} {
		w := apiRequest(h, "GET", path, "", true)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		var list apiNamespaceList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) != 1 || list.Items[0].ID != "test" {
			t.Fatal(list, err)
		}
	}
	w := apiRequest(h, "GET", "/api/v1/namespaces/other", "", true)
	if w.Code != 404 {
		t.Fatal(w.Code, w.Body)
	}
	spec := apiRequest(h, "GET", "/api/openapi.json", "", true)
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name     string
				Required bool
			}
		}
	}
	if err := json.Unmarshal(spec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for path, operations := range doc.Paths {
		for method, op := range operations {
			if method == "parameters" {
				continue
			}
			found := false
			for _, p := range op.Parameters {
				if p.Name == "X-Vmango-Namespace" {
					found = p.Required
				}
			}
			if found != workloadPath(path) {
				t.Errorf("%s %s namespace contract %v", method, path, found)
			}
		}
	}
}
func TestNamespaceDoesNotBecomeGlobalWhenRemoved(t *testing.T) {
	registry, _ := compute.NewNamespaceService(nil)
	env := &Environ{namespaces: registry, workloads: &compute.WorkloadService{}}
	called := false
	handler := env.namespaceGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	r := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	r.Header.Set("X-Vmango-Namespace", "removed")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if called || w.Code != 404 {
		t.Fatal("removed namespace reached workloads")
	}
	if got := requestNamespace(context.Background()); got != "" {
		t.Fatal(got)
	}
	if strings.Contains(w.Body.String(), "default") {
		t.Fatal(w.Body)
	}
}

func TestMissingWorkloadMembershipServiceFailsClosed(t *testing.T) {
	cfg := apiTestConfig(t)
	h, err := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil, testAuthService(t, cfg), newTestTaskService(t, nil), Services{Namespaces: testNamespaceRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	w := apiRequest(h, "GET", "/api/v1/clouds", "", true)
	if w.Code != 503 {
		t.Fatal("unconfigured membership service exposed workload API", w.Code, w.Body)
	}
	w = apiRequest(h, "GET", "/api/v1/namespaces", "", true)
	if w.Code != 200 {
		t.Fatal("membership storage blocked bootstrap discovery", w.Code, w.Body)
	}
}
