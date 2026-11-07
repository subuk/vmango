package api

import (
	"encoding/json"
	"github.com/rs/zerolog"
	"net/http/httptest"
	"path/filepath"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"
)

func TestAdministrationTaskFiltersAreIndependentAndPaginated(t *testing.T) {
	cfg := apiTestConfig(t)
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, taskAPIHandler{}, zerolog.Nop())
	samples := []struct{ ns, cloud, kind, scope, id string }{
		{"alpha", "west", "machine", "node", "a"},
		{"beta", "west", "volume", "node", "/b"},
		{"alpha", "east", "machine", "node", "c"},
		{"", "west", "machine", "elsewhere", "legacy"},
		{"beta", "west", "cloud", "node", "capacity"},
	}
	for _, sample := range samples {
		_, err := service.Submit(tasks.Identity{ID: "admin"}, "task-filter-key-"+sample.id, tasks.Command{NamespaceID: sample.ns, CloudID: sample.cloud, Kind: "machine.create", Payload: json.RawMessage(`{}`), Resources: []tasks.ResourceRef{{Kind: sample.kind, Scope: sample.scope, ID: sample.id}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	h, err := newScopedTestAPI(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, &compute.VirtualMachineManager{Clouds: &compute.CloudService{Definitions: []compute.Cloud{{ID: "local"}}}}, nil, testAuthService(t, cfg), service)
	if err != nil {
		t.Fatal(err)
	}
	get := func(query string) (tasks.Page, int) {
		t.Helper()
		r := httptest.NewRequest("GET", "https://vmango.test/api/v1/administration/tasks?"+query, nil)
		r.Header.Set("Authorization", "Bearer "+testWebToken())
		r.Header.Set("X-Vmango-Namespace", "unrelated")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Log(w.Body.String())
		}
		var page tasks.Page
		if w.Code == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		return page, w.Code
	}
	for _, test := range []struct {
		query string
		count int
	}{
		{"cloud_id=west", 4}, {"cloud_id=west&namespace_id=alpha", 1},
		{"unassigned=true", 1}, {"cloud_id=west&host=node", 2},
		{"cloud_id=east&host=node", 1}, {"namespace_id=removed", 0},
	} {
		page, code := get(test.query)
		if code != 200 || len(page.Items) != test.count {
			t.Fatalf("%s: code %d, page %+v", test.query, code, page)
		}
	}
	page, code := get("cloud_id=west&host=node&limit=1")
	if code != 200 || len(page.Items) != 1 || page.Next == "" {
		t.Fatal(code, page)
	}
	next, code := get("cloud_id=west&host=node&limit=1&before=" + page.Next)
	if code != 200 || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.Next != "" {
		t.Fatal(code, next)
	}
	for _, query := range []string{"unassigned=true&namespace_id=alpha", "host=node&resource_kind=machine", "state=not-a-state"} {
		if _, code := get(query); code != 422 {
			t.Fatal(query, code)
		}
	}
}
