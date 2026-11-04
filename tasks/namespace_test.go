package tasks_test

import (
	"context"
	"errors"
	"strings"
	"subuk/vmango/tasks"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

func namespaceCommand(namespace, id string) tasks.Command {
	c := command(id)
	c.NamespaceID = namespace
	c.CloudID = "shared"
	return c
}
func TestNamespaceTaskIsolationReplayCountersAndConflicts(t *testing.T) {
	r, _ := repository(t)
	s := tasks.New(r, handler{}, zerolog.Nop())
	a, err := s.Submit(identity, "namespace-idempotency-key", namespaceCommand("alpha", "first"))
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.Submit(identity, "namespace-idempotency-key", namespaceCommand("alpha", "first")); err != nil || replay.ID != a.ID {
		t.Fatal("same-context replay failed", replay, err)
	}
	if replay, err := s.Submit(identity, "namespace-idempotency-key", namespaceCommand("beta", "first")); !errors.Is(err, tasks.ErrKeyReuse) || replay.ID != "" {
		t.Fatal("cross-namespace replay exposed original", replay, err)
	}
	b, err := s.Submit(identity, "namespace-beta-second", namespaceCommand("beta", "second"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Submit(identity, "namespace-beta-conflict", namespaceCommand("beta", "first"))
	var conflict *tasks.ConflictError
	if !errors.As(err, &conflict) || conflict.TaskID != "" || strings.Contains(err.Error(), a.ID) {
		t.Fatal("foreign lock conflict leaked task", err)
	}
	_, err = s.Submit(identity, "namespace-alpha-conflict", namespaceCommand("alpha", "first"))
	if !errors.As(err, &conflict) || conflict.TaskID != a.ID {
		t.Fatal("same-context conflict lost task", err)
	}
	if _, err := s.Scoped("beta").Get(a.ID); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatal("cross-namespace get", err)
	}
	if _, err := s.Scoped("beta").Cancel(a.ID); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatal("cross-namespace cancel", err)
	}
	if _, err := s.Scoped("beta").Release(a.ID, identity, "inspected"); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatal("cross-namespace release", err)
	}
	page, err := s.Scoped("alpha").List(tasks.Filter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != a.ID || page.Active != 1 || page.Next != "" {
		t.Fatal("scoped counters/page", page, err)
	}
	admin, err := s.List(tasks.Filter{})
	if err != nil || len(admin.Items) != 2 || admin.Active != 2 {
		t.Fatal("administration lost history", admin, err)
	}
	restarted := tasks.New(r, handler{}, zerolog.Nop())
	stored, err := restarted.Scoped("beta").Get(b.ID)
	if err != nil || stored.NamespaceID != "beta" || stored.CloudID != "shared" {
		t.Fatal("attribution did not persist", stored, err)
	}
}
func TestRecoverInterruptedDoesNotStartQueuedWork(t *testing.T) {
	r, _ := repository(t)
	var runs atomic.Int32
	h := handler{run: func(context.Context, tasks.Command, *tasks.Execution) error { runs.Add(1); return nil }}
	s := tasks.New(r, h, zerolog.Nop())
	stale, err := s.Submit(identity, "stale-running-command", namespaceCommand("removed", "stale"))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.Submit(identity, "queued-unscoped-command", command("queued"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Update(func(store *tasks.Store) error {
		rec := store.Records[stale.ID]
		rec.Task.State = tasks.Running
		store.Records[stale.ID] = rec
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	a, err := s.Get(stale.ID)
	if err != nil || a.State != tasks.Interrupted || !a.OutcomeUncertain || !a.BlocksResources {
		t.Fatal("interruption safety lost", a, err)
	}
	b, err := s.Get(queued.ID)
	if err != nil || b.State != tasks.Queued || runs.Load() != 0 {
		t.Fatal("recovery executed queue", b, err)
	}
	if err := s.RecoverInterrupted(); err != nil {
		t.Fatal("repeat recovery", err)
	}
	after, _ := s.Get(stale.ID)
	if len(after.Transitions) != len(a.Transitions) {
		t.Fatal("recovery duplicated transition")
	}
	if _, err := s.Release(stale.ID, identity, "backend inspected after interruption"); err != nil {
		t.Fatal("offline release failed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if err := s.RecoverInterrupted(); !errors.Is(err, tasks.ErrInvalid) {
		t.Fatal("live executor could be recovered", err)
	}
}
