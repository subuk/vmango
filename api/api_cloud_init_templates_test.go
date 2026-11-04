package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func cloudInitTemplateAPI(t *testing.T) http.Handler {
	t.Helper()
	repo, err := filesystem.NewNamespaceCatalogRepository(filepath.Join(t.TempDir(), "templates"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	cfg := apiTestConfig(t)
	h, err := newScopedTestAPI(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, nil, nil, testAuthService(t, cfg), newTestTaskService(t, &compute.CommandHandler{}), Services{Namespaces: testNamespaceRegistry(), CloudInitTemplates: compute.NewNamespaceCloudInitTemplateService(repo, testNamespaceRegistry())})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCloudInitTemplateAPICatalogAndRevisions(t *testing.T) {
	h := cloudInitTemplateAPI(t)
	const path = "/api/v1/cloud-init-templates"
	const userData = "#cloud-config\npassword: private-template-secret\n"
	createBody, _ := json.Marshal(map[string]string{"name": "Bootstrap", "description": "Initial configuration", "user_data": userData})
	w := apiRequest(h, "GET", path, "", true)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("fresh catalog: %d %s", w.Code, w.Body)
	}
	w = apiRequest(h, "POST", path, string(createBody), true)
	if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created apiCloudInitTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Revision == "" || created.UserData != userData || created.Name != "Bootstrap" {
		t.Fatalf("incomplete response: %+v", created)
	}
	itemPath := path + "/" + created.ID
	w = apiRequest(h, "GET", path, "", true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "user_data") || strings.Contains(w.Body.String(), "private-template-secret") || !strings.Contains(w.Body.String(), created.ID) {
		t.Fatalf("catalog disclosed user-data or lost metadata: %d %s", w.Code, w.Body)
	}
	w = apiRequest(h, "GET", itemPath, "", true)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "private-template-secret") {
		t.Fatalf("detail: %d %s", w.Code, w.Body)
	}
	updateBody, _ := json.Marshal(map[string]string{"name": "Updated bootstrap", "user_data": "#!/bin/sh\nprintf updated\\n\n", "revision": created.Revision})
	w = apiRequest(h, "PUT", itemPath, string(updateBody), true)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	var updated apiCloudInitTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Revision == created.Revision || updated.Name != "Updated bootstrap" {
		t.Fatalf("update did not preserve identity and change revision: %+v", updated)
	}
	for _, method := range []string{"PUT", "DELETE"} {
		body := string(updateBody)
		if method == "DELETE" {
			body = fmt.Sprintf(`{"revision":%q}`, created.Revision)
		}
		w = apiRequest(h, method, itemPath, body, true)
		if w.Code != 409 {
			t.Fatalf("stale %s: %d %s", method, w.Code, w.Body)
		}
	}
	w = apiRequest(h, "DELETE", itemPath, fmt.Sprintf(`{"revision":%q}`, updated.Revision), true)
	if w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	w = apiRequest(h, "GET", itemPath, "", true)
	if w.Code != 404 {
		t.Fatalf("deleted template remained accessible: %d %s", w.Code, w.Body)
	}
}

func TestCloudInitTemplateAPIAuthenticationAndOrigins(t *testing.T) {
	h := cloudInitTemplateAPI(t)
	const path = "/api/v1/cloud-init-templates"
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		url := path
		if method == "PUT" || method == "DELETE" {
			url += "/" + uuid.NewString()
		}
		w := apiRequest(h, method, url, `{}`, false)
		if w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", method, w.Code)
		}
		r := httptest.NewRequest(method, "https://vmango.test"+url, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+testWebToken())
		r.Header.Set("Origin", "https://evil.test")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("cross-origin %s: %d", method, w.Code)
		}
	}
	w := apiRequest(h, "GET", path, "", true)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("rejected requests mutated catalog: %d %s", w.Code, w.Body)
	}
}

func TestCloudInitTemplateAPIValidationDoesNotEchoContents(t *testing.T) {
	h := cloudInitTemplateAPI(t)
	const secret = "private-bootstrap-secret"
	for _, input := range []map[string]any{
		{"name": "test", "user_data": map[string]string{"password": secret}},
		{"name": "", "user_data": secret},
		{"name": "test", "user_data": strings.Repeat(secret, compute.MaxCloudInitUserDataBytes/len(secret)+1)},
	} {
		body, _ := json.Marshal(input)
		w := apiRequest(h, "POST", "/api/v1/cloud-init-templates", string(body), true)
		if w.Code != 422 || strings.Contains(w.Body.String(), secret) {
			t.Fatalf("validation leaked user-data or accepted invalid input: %d %.500s", w.Code, w.Body)
		}
	}
}

type templateSubmissionHandler struct {
	taskAPIHandler
	received compute.InfrastructureCommand
}

func (h *templateSubmissionHandler) Prepare(command tasks.Command) (tasks.Command, error) {
	return command, json.Unmarshal(command.Payload, &h.received)
}

func TestCloudInitTemplateAPIMachineSubmission(t *testing.T) {
	cfg := apiTestConfig(t)
	handler := &templateSubmissionHandler{}
	clouds := &compute.CloudService{Definitions: []compute.Cloud{{ID: "local"}}}
	h, err := newScopedTestAPI(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, &compute.VirtualMachineManager{Clouds: clouds}, nil, testAuthService(t, cfg), newTestTaskService(t, handler))
	if err != nil {
		t.Fatal(err)
	}
	id, revision := uuid.NewString(), uuid.NewString()
	body := fmt.Sprintf(`{"name":"vm","image":"ubuntu","instance_type":"small","zone":"z","network":"lan","storage_class":"fast","start":false,"user_data_template":{"id":%q,"revision":%q}}`, id, revision)
	w := apiRequest(h, "POST", "/api/v1/clouds/local/machines", body, true)
	if w.Code != 202 {
		t.Fatalf("submit template reference: %d %s", w.Code, w.Body)
	}
	spec := handler.received.CreateSpec
	if handler.received.CloudID != "local" || spec == nil || spec.UserData != "" || spec.UserDataTemplate == nil || spec.UserDataTemplate.ID != id || spec.UserDataTemplate.Revision != revision {
		t.Fatalf("template reference was lost at the API boundary: %+v", spec)
	}
}
