package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type handler struct {
	prepare func(tasks.Command) (tasks.Command, error)
	run     func(context.Context, tasks.Command, *tasks.Execution) error
}

func (h handler) Prepare(c tasks.Command) (tasks.Command, error) {
	if h.prepare != nil {
		return h.prepare(c)
	}
	return c, nil
}
func (h handler) Execute(ctx context.Context, c tasks.Command, e *tasks.Execution) error {
	if h.run != nil {
		return h.run(ctx, c, e)
	}
	return nil
}

var identity = tasks.Identity{ID: "local/admin", Name: "Admin"}

func command(id string) tasks.Command {
	return tasks.Command{Kind: "machine.create", Payload: json.RawMessage(`{"user_data":"password: bootstrap-private-secret"}`), Resources: []tasks.ResourceRef{{Kind: "machine", Scope: "n", ID: id}}}
}
func repository(t *testing.T) (*filesystem.TaskRepository, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tasks")
	r, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, dir
}
func waitTask(t *testing.T, s *tasks.Service, id string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.State.Terminal() {
			return task
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not finish")
	return tasks.Task{}
}
func start(t *testing.T, s *tasks.Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); s.Wait() })
}

func TestDurableConcurrentDeduplicationCancellationAndConflicts(t *testing.T) {
	r, _ := repository(t)
	s := tasks.New(r, handler{}, zerolog.Nop())
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := s.Submit(identity, "same-submission-key", command("v"))
			if err != nil {
				t.Error(err)
				return
			}
			ids <- task.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for next := range ids {
		if id != "" && id != next {
			t.Fatal("duplicate task")
		}
		id = next
	}
	changed := command("v")
	changed.Payload = json.RawMessage(`{"different":true}`)
	if _, err := s.Submit(identity, "same-submission-key", changed); !errors.Is(err, tasks.ErrKeyReuse) {
		t.Fatal(err)
	}
	if _, err := s.Submit(identity, "different-submission-key", command("v")); err == nil {
		t.Fatal("conflicting command accepted")
	} else {
		var conflict *tasks.ConflictError
		if !errors.As(err, &conflict) || conflict.TaskID != id {
			t.Fatal(err)
		}
	}
	other, err := s.Submit(identity, "unrelated-submission", command("other"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cancel(id); err != nil {
		t.Fatal(err)
	}
	store, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Records) != 2 || len(store.Records[id].Payload) != 0 || store.Records[id].Task.BlocksResources {
		t.Fatal("cancel was not committed with payload cleanup and lock release")
	}
	start(t, s)
	if task := waitTask(t, s, other.ID); task.State != tasks.Succeeded {
		t.Fatal(task)
	}
	if task, err := s.Get(id); err != nil || task.State != tasks.Cancelled || len(task.Stages) != 0 {
		t.Fatal(task, err)
	}
}

func TestRestartInterruptsRunningAndExecutesOnlyNeverStartedQueue(t *testing.T) {
	r, dir := repository(t)
	s := tasks.New(r, handler{}, zerolog.Nop())
	running, err := s.Submit(identity, "running-submission", command("running"))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.Submit(identity, "queued-submission", command("queued"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := r.Update(func(store *tasks.Store) error {
		rec := store.Records[running.ID]
		rec.Task.State = tasks.Running
		rec.Task.StartedAt = &now
		rec.Task.Stages = []tasks.Stage{{Name: "define-vm", StartedAt: now, State: "running"}}
		rec.Task.Effects = []tasks.Effect{{Resource: tasks.ResourceRef{Kind: "volume", Scope: "n", ID: "/root"}, Action: "created", At: now}}
		store.Records[running.ID] = rec
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r.Close()
	reopened, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var calls atomic.Int32
	s2 := tasks.New(reopened, handler{run: func(_ context.Context, c tasks.Command, e *tasks.Execution) error {
		calls.Add(1)
		if c.Resources[0].ID != "queued" {
			t.Error("interrupted task replayed")
		}
		return e.Stage("execute", &c.Resources[0], true, func() error { return e.Effect(c.Resources[0], "created") })
	}}, zerolog.Nop())
	start(t, s2)
	interrupted, err := s2.Get(running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.State != tasks.Interrupted || !interrupted.OutcomeUncertain || !interrupted.BlocksResources || len(interrupted.Stages) != 1 || len(interrupted.Effects) != 1 || interrupted.Error == nil || !strings.Contains(interrupted.Error.Message, "No backend failure cause is known") {
		t.Fatalf("lost interruption history: %+v", interrupted)
	}
	if task, err := s2.Submit(identity, "running-submission", command("running")); err != nil || task.ID != running.ID {
		t.Fatal(task, err)
	}
	if _, err := s2.Submit(identity, "new-running-submission", command("running")); err == nil {
		t.Fatal("interrupted lock silently released")
	}
	if task := waitTask(t, s2, queued.ID); task.State != tasks.Succeeded || calls.Load() != 1 {
		t.Fatal(task, calls.Load())
	}
	if _, err := s2.Release(running.ID, identity, "Inspected backend; no action remains running"); err != nil {
		t.Fatal(err)
	}
	if task, err := s2.Get(running.ID); err != nil || task.State != tasks.Interrupted || task.BlocksResources || task.LockRelease == nil {
		t.Fatal(task, err)
	}
	if err := s2.Cleanup(time.Now().Add(48*time.Hour), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	page, err := s2.List(tasks.Filter{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
}

func TestPartialFailureRetainsSanitizedDiagnosticsAndHistoryAfterReopen(t *testing.T) {
	r, dir := repository(t)
	ref := tasks.ResourceRef{Kind: "volume", Scope: "n", ID: "/created"}
	backend := &tasks.DiagnosticError{Details: tasks.Diagnostic{Message: "QEMU: disk image is locked; password=bootstrap-private-secret token=api-secret", Backend: "libvirt", Code: "38", Domain: "10", Operation: "virDomainCreate"}, Cause: errors.New("original")}
	s := tasks.New(r, handler{run: func(_ context.Context, c tasks.Command, e *tasks.Execution) error {
		if err := e.Stage("create-volume", &ref, true, func() error { return e.Effect(ref, "created") }); err != nil {
			return err
		}
		return e.Stage("start-vm", &c.Resources[0], true, func() error { return fmtWrap(backend) })
	}}, zerolog.Nop())
	accepted, err := s.Submit(identity, "partial-submission", command("v"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	task := waitTask(t, s, accepted.ID)
	cancel()
	s.Wait()
	if task.State != tasks.Failed || !task.OutcomeUncertain || len(task.Stages) != 2 || len(task.Effects) != 1 || task.Error.Code != "38" || task.Error.Domain != "10" || task.Error.Operation != "virDomainCreate" || !strings.Contains(task.Error.Message, "disk image is locked") {
		t.Fatalf("lost cause: %+v", task)
	}
	public, _ := json.Marshal(task)
	if strings.Contains(string(public), "bootstrap-private-secret") || strings.Contains(string(public), "api-secret") || strings.Contains(string(public), "payload") {
		t.Fatal(string(public))
	}
	r.Close()
	reopened, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err := reopened.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Records[task.ID].Payload) != 0 {
		t.Fatal("terminal payload retained")
	}
	if err := tasks.New(reopened, handler{}, zerolog.Nop()).Cleanup(time.Now().Add(48*time.Hour), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	stored, _ = reopened.Read()
	if len(stored.Records) != 1 {
		t.Fatal("retention removed unresolved locks")
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), "bootstrap-private-secret") || strings.Contains(string(bytes), "api-secret") {
		t.Fatal("secret survived terminal persistence")
	}
}
func fmtWrap(err error) error { return &wrapped{err} }

type wrapped struct{ cause error }

func (e *wrapped) Error() string { return "cannot create VM" }
func (e *wrapped) Unwrap() error { return e.cause }

type failStageRepository struct{ tasks.Repository }

func (r failStageRepository) Update(fn func(*tasks.Store) error) error {
	return r.Repository.Update(func(s *tasks.Store) error {
		if err := fn(s); err != nil {
			return err
		}
		for _, rec := range s.Records {
			if len(rec.Task.Stages) > 0 {
				return errors.New("disk full")
			}
		}
		return nil
	})
}
func TestHistoryWriteFailurePreventsBackendCall(t *testing.T) {
	r, _ := repository(t)
	var writes atomic.Int32
	s := tasks.New(failStageRepository{r}, handler{run: func(_ context.Context, c tasks.Command, e *tasks.Execution) error {
		return e.Stage("create", &c.Resources[0], true, func() error { writes.Add(1); return nil })
	}}, zerolog.Nop())
	task, err := s.Submit(identity, "storage-failure-key", command("v"))
	if err != nil {
		t.Fatal(err)
	}
	start(t, s)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Submit(identity, "probe-submission", command("other")); err != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if writes.Load() != 0 {
		t.Fatal("backend ran without persisted stage intent")
	}
	stored, err := s.Get(task.ID)
	if err != nil || stored.State != tasks.Running || !stored.BlocksResources {
		t.Fatal(stored, err)
	}
}

func TestTaskRepositoryExcludesOtherProcessesAndUnsafeFiles(t *testing.T) {
	r, dir := repository(t)
	if second, err := filesystem.NewTaskRepository(dir); err == nil {
		second.Close()
		t.Fatal("second executor acquired store")
	}
	r.Close()
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(`{"version":999,"records":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := filesystem.NewTaskRepository(dir); err == nil {
		reopened.Close()
		t.Fatal("unsupported history silently discarded")
	}
}

func TestShutdownDoesNotStartAnotherBackendStage(t *testing.T) {
	repo, _ := repository(t)
	entered := make(chan struct{})
	complete := make(chan struct{})
	var calls atomic.Int32
	service := tasks.New(repo, handler{run: func(_ context.Context, c tasks.Command, e *tasks.Execution) error {
		if err := e.Stage("first-stage", &c.Resources[0], true, func() error { calls.Add(1); close(entered); <-complete; return e.Effect(c.Resources[0], "created") }); err != nil {
			return err
		}
		return e.Stage("second-stage", &c.Resources[0], true, func() error { calls.Add(1); return nil })
	}}, zerolog.Nop())
	task, err := service.Submit(identity, "shutdown-submission", command("shutdown"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	<-entered
	cancel()
	close(complete)
	service.Wait()
	task, err = service.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != tasks.Interrupted || calls.Load() != 1 || len(task.Stages) != 1 || len(task.Effects) != 1 {
		t.Fatalf("shutdown continued stages or lost history: %+v calls=%d", task, calls.Load())
	}
}
