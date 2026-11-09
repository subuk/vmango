package compute_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestPreUpgradeTasksFailWithoutExecutionAndPreserveFences(t *testing.T) {
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	locks := []tasks.ResourceRef{{Kind: "machine", Scope: "host", ID: "vm"}}
	effect := tasks.Effect{Resource: locks[0], Action: "created"}
	now := time.Now().UTC()
	terminal := tasks.Record{Task: tasks.Task{ID: "terminal", State: tasks.Succeeded, CreatedAt: now, FinishedAt: &now, Effects: []tasks.Effect{effect}}, Payload: json.RawMessage(`{"Version":3}`), Locks: locks, KeyHash: "terminal", Fingerprint: "terminal"}
	if err := repo.Update(func(store *tasks.Store) error {
		store.Records["queued"] = tasks.Record{Task: tasks.Task{ID: "queued", Kind: "machine.create", State: tasks.Queued, CreatedAt: now, BlocksResources: true}, Payload: json.RawMessage(`{"Version":4,"Machine":"deliberately not decodable"}`), Locks: locks, KeyHash: "queued", Fingerprint: "queued"}
		store.Records["running"] = tasks.Record{Task: tasks.Task{ID: "running", Kind: "machine.create", State: tasks.Running, CreatedAt: now, BlocksResources: true, Effects: []tasks.Effect{effect}}, Payload: json.RawMessage(`{"Version":4}`), Locks: locks, KeyHash: "running", Fingerprint: "running"}
		store.Records["terminal"] = terminal
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A handler without any backend wiring must be safe: migration reads only the
	// envelope and never reconstructs or dispatches either old command.
	service := tasks.New(repo, &compute.CommandHandler{}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	store, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"queued", "running"} {
		record := store.Records[id]
		if record.Task.State != tasks.Failed || record.Task.Error == nil || !strings.Contains(record.Task.Error.Message, "incompatible upgrade") || len(record.Task.Stages) != 0 || len(record.Payload) != 0 {
			t.Fatalf("unexpected migration: %+v", record)
		}
		if !reflect.DeepEqual(record.Locks, locks) {
			t.Fatal("lost resource identities")
		}
	}
	if store.Records["queued"].Task.BlocksResources {
		t.Fatal("never started queue retains a needless fence")
	}
	running := store.Records["running"].Task
	if !running.BlocksResources || !running.OutcomeUncertain || !reflect.DeepEqual(running.Effects, []tasks.Effect{effect}) {
		t.Fatal("lost uncertain effects or fence", running)
	}
	if !reflect.DeepEqual(store.Records["terminal"], terminal) {
		t.Fatal("terminal history changed")
	}
}
