package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type taskAPIHandler struct{}

func (taskAPIHandler) Prepare(c tasks.Command) (tasks.Command, error) { return c, nil }
func (taskAPIHandler) Execute(_ context.Context, c tasks.Command, e *tasks.Execution) error {
	return e.Stage("define-vm", &c.Resources[0], true, func() error { return e.Effect(c.Resources[0], "created") })
}

func TestTaskAPIIdempotentAcceptanceCancellationAndRestartHistory(t *testing.T) {
	cfg := apiTestConfig(t)
	dir := filepath.Join(t.TempDir(), "tasks")
	repo, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := tasks.New(repo, taskAPIHandler{}, zerolog.Nop())
	authentication := testAuthService(t, cfg)
	newAPI := func(s *tasks.Service) http.Handler {
		t.Helper()
		h, err := New(cfg, zerolog.Nop(), nil, nil, nil, nil, nil, nil, &compute.VirtualMachineManager{Clouds: &compute.CloudService{}}, nil, authentication, s)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	h := newAPI(service)
	submit := func(key, body string) *httptest.ResponseRecorder {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := httptest.NewRequest("POST", "https://vmango.test/api/v1/clouds/local/machines", strings.NewReader(body)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+testWebToken())
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"id":"v","image":"ubuntu","instance_type":"small","zone":"z","network":"lan","storage_class":"fast","start":false,"user_data":"private-bootstrap-secret"}`
	first := submit("recoverable-submission", body)
	if first.Code != 202 || first.Header().Get("Location") == "" || strings.Contains(first.Body.String(), "private-bootstrap-secret") {
		t.Fatalf("acceptance: %d %s", first.Code, first.Body)
	}
	var accepted apiTaskReference
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	second := submit("recoverable-submission", body)
	var duplicate apiTaskReference
	_ = json.Unmarshal(second.Body.Bytes(), &duplicate)
	if second.Code != 202 || duplicate.Task.ID != accepted.Task.ID {
		t.Fatal("duplicate submission", second.Body)
	}
	if response := submit("recoverable-submission", strings.Replace(body, `"instance_type":"small"`, `"instance_type":"large"`, 1)); response.Code != 409 {
		t.Fatal(response.Code, response.Body)
	}
	if response := submit("conflicting-submission", body); response.Code != 409 || !strings.Contains(response.Body.String(), accepted.Task.ID) {
		t.Fatal(response.Code, response.Body)
	}
	for _, path := range []string{"/api/v1/tasks", "/api/v1/tasks/" + accepted.Task.ID} {
		if response := apiRequest(h, "GET", path, "", false); response.Code != 401 {
			t.Fatal("unguarded task endpoint", response.Code)
		}
	}
	if response := apiRequest(h, "POST", accepted.StatusURL+"/cancel", "", true); response.Code != 200 {
		t.Fatal(response.Code, response.Body)
	}
	if response := submit("recoverable-submission", body); response.Code != 202 || !strings.Contains(response.Body.String(), `"state":"cancelled"`) {
		t.Fatal(response.Code, response.Body)
	}
	queuedResponse := submit("queued-after-cancel", strings.Replace(body, `"id":"v"`, `"id":"q"`, 1))
	var queued apiTaskReference
	_ = json.Unmarshal(queuedResponse.Body.Bytes(), &queued)
	staleResponse := submit("interrupted-after-restart", strings.Replace(body, `"id":"v"`, `"id":"stale"`, 1))
	var stale apiTaskReference
	_ = json.Unmarshal(staleResponse.Body.Bytes(), &stale)
	if queuedResponse.Code != 202 || staleResponse.Code != 202 {
		t.Fatal(queuedResponse.Body, staleResponse.Body)
	}
	if err := repo.Update(func(store *tasks.Store) error {
		rec := store.Records[stale.Task.ID]
		rec.Task.State = tasks.Running
		now := time.Now().UTC()
		rec.Task.StartedAt = &now
		rec.Task.Stages = []tasks.Stage{{Name: "define-vm", State: "running", StartedAt: now}}
		store.Records[stale.Task.ID] = rec
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	reopened, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service2 := tasks.New(reopened, taskAPIHandler{}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service2.Wait() }()
	if err := service2.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	h = newAPI(service2)
	awaitAPITask(t, h, queuedResponse)
	interrupted := apiRequest(h, "GET", stale.StatusURL, "", true)
	if interrupted.Code != 200 || !strings.Contains(interrupted.Body.String(), `"state":"interrupted"`) || !strings.Contains(interrupted.Body.String(), `"outcome_uncertain":true`) || strings.Contains(interrupted.Body.String(), "private-bootstrap-secret") {
		t.Fatal(interrupted.Body)
	}
	if response := submit("interrupted-after-restart", strings.Replace(body, `"id":"v"`, `"id":"stale"`, 1)); response.Code != 202 || !strings.Contains(response.Body.String(), stale.Task.ID) {
		t.Fatal(response.Code, response.Body)
	}
	page := apiRequest(h, "GET", "/api/v1/tasks?limit=1&resource_kind=machine-request&resource_scope=local", "", true)
	var history tasks.Page
	if err := json.Unmarshal(page.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if page.Code != 200 || len(history.Items) != 1 || history.Next == "" {
		t.Fatal(page.Code, page.Body)
	}
	if response := apiRequest(h, "GET", "/api/v1/tasks?state=cancelled&resource_id=v", "", true); response.Code != 200 || !strings.Contains(response.Body.String(), accepted.Task.ID) {
		t.Fatal(response.Code, response.Body)
	}
	if response := apiRequest(h, "POST", stale.StatusURL+"/release", `{"reason":"Inspected actual backend resources"}`, true); response.Code != 200 || !strings.Contains(response.Body.String(), `"blocks_resources":false`) {
		t.Fatal(response.Code, response.Body)
	}
}
