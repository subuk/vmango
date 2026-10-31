package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"subuk/vmango/compute"
	"testing"

	"github.com/rs/zerolog"
)

type administrationAPIBackend struct {
	compute.AdministrationObservations
	calls int
	fail  map[string]bool
}

func (b *administrationAPIBackend) Host(_ context.Context, _ string, node string) (*compute.Node, error) {
	b.calls++
	if b.fail[node] {
		return nil, compute.ErrUnavailable
	}
	return &compute.Node{Id: node, CpuArch: compute.ArchAmd64}, nil
}
func administrationAPI(t *testing.T, snapshot compute.AdministrationSnapshot, backends map[string]compute.AdministrationObservations) http.Handler {
	t.Helper()
	cfg := apiTestConfig(t)
	h, err := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil, testAuthService(t, cfg), newTestTaskService(t, &compute.CommandHandler{}), compute.NewAdministrationService(snapshot, backends))
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestAdministrationHostlessAPIAndRemovedPaths(t *testing.T) {
	snapshot := compute.AdministrationSnapshot{Installation: compute.AdministrationInstallation{SnapshotID: "snapshot", Activation: "restart"}, Clouds: []compute.AdministrationCloud{{ID: "external.v1", Name: "External", BackendKind: "external", Images: []string{"same"}}}, Images: []compute.AdministrationImage{{ID: "same", CloudID: "external.v1", Name: "External image", Arch: "aarch64"}}}
	h := administrationAPI(t, snapshot, nil)
	for _, path := range []string{"/api/v1/administration/configuration", "/api/v1/administration/clouds", "/api/v1/administration/clouds/external.v1", "/api/v1/administration/clouds/external.v1/configuration", "/api/v1/administration/clouds/external.v1/images", "/api/v1/administration/clouds/external.v1/images/same"} {
		w := apiRequest(h, "GET", path, "", true)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body)
		}
		if w = apiRequest(h, "GET", path, "", false); w.Code != 401 {
			t.Fatal("unauthenticated admin read", path, w.Code)
		}
		r := httptest.NewRequest("GET", "https://vmango.test"+path, nil)
		r.Header.Set("Authorization", "Bearer "+testWebToken())
		r.Header.Set("X-Vmango-Namespace", "ignored")
		r.Header.Set("X-Vmango-Context", "cloud-admin")
		withHeaders := httptest.NewRecorder()
		h.ServeHTTP(withHeaders, r)
		without := apiRequest(h, "GET", path, "", true)
		if withHeaders.Code != 200 || withHeaders.Body.String() != without.Body.String() {
			t.Fatal("workspace headers affected accepted configuration", path, withHeaders.Body, without.Body)
		}
	}
	for _, path := range []string{"/api/v1/hosts", "/api/v1/hosts/a", "/api/v1/hosts/a/networks", "/api/v1/hosts/a/pools", "/api/v1/hosts/a/capabilities", "/api/v1/hosts/a/catalog", "/api/v1/hosts/a/images", "/api/v1/hosts/a/cloud", "/api/v1/administration/clouds/external.v1/hosts", "/api/v1/administration/clouds/external.v1/hosts/a", "/api/v1/administration/clouds/external.v1/configuration/hosts", "/api/v1/administration/clouds/external.v1/images/same/availability", "/api/v1/administration/clouds/missing"} {
		if w := apiRequest(h, "GET", path, "", true); w.Code != 404 {
			t.Fatal("unsupported/retired route", path, w.Code, w.Body)
		}
	}
	if w := apiRequest(h, "GET", "/api/v1/administration/clouds/external.v1/images?arch=invalid", "", true); w.Code != 422 {
		t.Fatal(w.Code, w.Body)
	}
	if w := apiRequest(h, "GET", "/api/v1/administration/clouds/external.v1/images?arch=x86_64", "", true); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Code, w.Body)
	}
	if w := apiRequest(h, "POST", "/api/v1/administration/configuration", `{}`, true); w.Code != 405 {
		t.Fatal("configuration mutation accepted", w.Code, w.Body)
	}
}
func TestAdministrationMembershipAndPartialHostResponses(t *testing.T) {
	b := &administrationAPIBackend{fail: map[string]bool{"offline": true}}
	snapshot := compute.AdministrationSnapshot{Clouds: []compute.AdministrationCloud{{ID: "a", Capabilities: compute.AdministrationCapabilities{HostInventory: true}}, {ID: "b", Capabilities: compute.AdministrationCapabilities{HostInventory: true}}}, Hosts: []compute.AdministrationHost{{ID: "online", CloudID: "a"}, {ID: "offline", CloudID: "a"}, {ID: "other", CloudID: "b"}}}
	h := administrationAPI(t, snapshot, map[string]compute.AdministrationObservations{"a": b, "b": b})
	for _, path := range []string{"/api/v1/administration/clouds/a/configuration", "/api/v1/administration/clouds/a/configuration/hosts", "/api/v1/administration/clouds/a/configuration/hosts/offline"} {
		if w := apiRequest(h, "GET", path, "", true); w.Code != 200 {
			t.Fatal(path, w.Code, w.Body)
		}
	}
	if b.calls != 0 {
		t.Fatal("configuration contacted a host")
	}
	for _, path := range []string{"/api/v1/administration/clouds/a/hosts/other", "/api/v1/administration/clouds/a/hosts/missing", "/api/v1/administration/clouds/missing/hosts/online"} {
		if w := apiRequest(h, "GET", path, "", true); w.Code != 404 {
			t.Fatal(path, w.Code, w.Body)
		}
	}
	if b.calls != 0 {
		t.Fatal("membership checked after host access")
	}
	w := apiRequest(h, "GET", "/api/v1/administration/clouds/a/hosts", "", true)
	var out apiAdministrationHostList
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(out.Items) != 1 || len(out.UnavailableHosts) != 1 || out.UnavailableHosts[0].ID != "offline" {
		t.Fatal(w.Code, w.Body)
	}
	if w := apiRequest(h, "GET", "/api/v1/administration/clouds/a/hosts/offline", "", true); w.Code != 503 {
		t.Fatal(w.Code, w.Body)
	}
	b.fail["online"] = true
	w = apiRequest(h, "GET", "/api/v1/administration/clouds/a/hosts", "", true)
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || out.Items == nil || len(out.Items) != 0 || len(out.UnavailableHosts) != 2 {
		t.Fatal(w.Code, w.Body)
	}
}
