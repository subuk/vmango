package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Service struct {
	repo    Repository
	handler Handler
	logger  zerolog.Logger
	mu      sync.Mutex
	wake    chan struct{}
	poison  error
	wg      sync.WaitGroup
	started bool
}

func New(repo Repository, handler Handler, logger zerolog.Logger) *Service {
	return &Service{repo: repo, handler: handler, logger: logger, wake: make(chan struct{}, 1)}
}

func hash(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }
func conflict(store Store, refs []ResourceRef) *ConflictError {
	for _, rec := range store.Records {
		if !rec.Task.BlocksResources {
			continue
		}
		for _, a := range rec.Locks {
			for _, b := range refs {
				if a.Kind == b.Kind && a.Scope == b.Scope && a.ID == b.ID && (!a.ReadOnly || !b.ReadOnly || rec.Task.OutcomeUncertain) {
					return &ConflictError{rec.Task.ID}
				}
			}
		}
	}
	return nil
}
func unique(refs []ResourceRef) []ResourceRef {
	out := []ResourceRef{}
	seen := map[ResourceRef]bool{}
	for _, r := range refs {
		if !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	return out
}
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) lockRefs(refs []ResourceRef) []ResourceRef {
	if resolver, ok := s.handler.(LockResolver); ok {
		return unique(resolver.LockRefs(refs))
	}
	return unique(refs)
}

// Submit deduplicates before validation or backend reads. Personal credentials
// and web sessions share a principal's key namespace, independent of token expiry.
func (s *Service) Submit(who Identity, key string, command Command) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poison != nil {
		return Task{}, s.poison
	}
	if who.ID == "" || len(key) < 8 || len(key) > 200 || strings.TrimSpace(key) != key {
		return Task{}, ErrInvalid
	}
	keyHash := hash(who.ID + "\x00" + key)
	fingerprint := hash(command.Kind + "\x00" + string(command.Payload))
	store, err := s.repo.Read()
	if err != nil {
		return Task{}, fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
	}
	for _, rec := range store.Records {
		if rec.KeyHash == keyHash {
			if rec.Fingerprint != fingerprint {
				return Task{}, ErrKeyReuse
			}
			return rec.Task, nil
		}
	}
	if c := conflict(store, s.lockRefs(command.Resources)); c != nil {
		return Task{}, c
	}
	command, err = s.handler.Prepare(command)
	if err != nil {
		return Task{}, err
	}
	command.Resources = unique(command.Resources)
	if c := conflict(store, s.lockRefs(command.Resources)); c != nil {
		return Task{}, c
	}
	now := time.Now().UTC()
	task := Task{ID: uuid.NewString(), Kind: command.Kind, State: Queued, Initiator: who, CreatedAt: now, Resources: command.Resources, Stages: []Stage{}, Transitions: []Transition{{State: Queued, At: now}}, Effects: []Effect{}, BlocksResources: true}
	err = s.repo.Update(func(state *Store) error {
		state.Records[task.ID] = Record{Task: task, Payload: command.Payload, Locks: s.lockRefs(command.Resources), KeyHash: keyHash, Fingerprint: fingerprint, ExecutionGroups: command.ExecutionGroups}
		return nil
	})
	if err != nil {
		s.poison = fmt.Errorf("%w; restart required: %w", ErrStorageUnavailable, err)
		return Task{}, s.poison
	}
	s.logger.Info().Str("task_id", task.ID).Str("kind", task.Kind).Msg("task accepted")
	s.signal()
	return task, nil
}

func (s *Service) Get(id string) (Task, error) {
	store, err := s.repo.Read()
	if err != nil {
		return Task{}, fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
	}
	rec, ok := store.Records[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	return rec.Task, nil
}

func (s *Service) List(f Filter) (Page, error) {
	store, err := s.repo.Read()
	if err != nil {
		return Page{}, fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
	}
	page := Page{Items: []Task{}}
	all := []Task{}
	for _, rec := range store.Records {
		t := rec.Task
		if !t.State.Terminal() {
			page.Active++
		}
		if f.Blocking && !t.BlocksResources {
			continue
		}
		if len(f.States) > 0 {
			found := false
			for _, state := range f.States {
				if state == t.State {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if f.Resource != nil {
			found := false
			for _, r := range t.Resources {
				if (f.Resource.Kind == "" || r.Kind == f.Resource.Kind) && (f.Resource.Scope == "" || r.Scope == f.Resource.Scope) && (f.Resource.ID == "" || r.ID == f.Resource.ID) {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if f.Before != "" && cursor(t) >= f.Before {
			continue
		}
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool { return cursor(all[i]) > cursor(all[j]) })
	limit := f.Limit
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if len(all) > limit {
		page.Next = cursor(all[limit-1])
		all = all[:limit]
	}
	page.Items = all
	return page, nil
}
func cursor(t Task) string { return t.CreatedAt.Format("20060102T150405.000000000") + "/" + t.ID }

func (s *Service) change(id string, fn func(*Record) error) error {
	if s.poison != nil {
		return s.poison
	}
	err := s.repo.Update(func(store *Store) error {
		rec, ok := store.Records[id]
		if !ok {
			return ErrNotFound
		}
		if err := fn(&rec); err != nil {
			return err
		}
		store.Records[id] = rec
		return nil
	})
	if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrNotQueued) && !errors.Is(err, ErrInvalid) {
		s.poison = fmt.Errorf("%w; execution fenced until restart: %w", ErrStorageUnavailable, err)
		return s.poison
	}
	return err
}

func finish(rec *Record, state State, reason string) {
	now := time.Now().UTC()
	rec.Task.State, rec.Task.FinishedAt = state, &now
	rec.Task.Transitions = append(rec.Task.Transitions, Transition{state, now, reason})
	rec.Task.BlocksResources = rec.Task.OutcomeUncertain
	// Keep only the fingerprint for deduplication after terminal completion.
	rec.Payload = nil
}
func (s *Service) Cancel(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.change(id, func(rec *Record) error {
		if rec.Task.State == Cancelled {
			return nil
		}
		if rec.Task.State != Queued {
			return ErrNotQueued
		}
		finish(rec, Cancelled, "Cancelled before execution; no backend action was started.")
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(id)
}

func (s *Service) Release(id string, who Identity, reason string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if who.ID == "" || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 {
		return Task{}, ErrInvalid
	}
	err := s.change(id, func(rec *Record) error {
		if !rec.Task.State.Terminal() || !rec.Task.OutcomeUncertain {
			return ErrInvalid
		}
		if rec.Task.LockRelease != nil {
			return nil
		}
		rec.Task.LockRelease = &Release{who, time.Now().UTC(), Redact(reason, nil)}
		rec.Task.BlocksResources = false
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(id)
}

// Start first marks stale running tasks interrupted, then starts a bounded pool.
// Queued work retains the authorization granted when it was durably accepted.
func (s *Service) Start(ctx context.Context, workers int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || workers < 1 || workers > 32 {
		return ErrInvalid
	}
	if err := s.repo.Update(func(store *Store) error {
		for id, rec := range store.Records {
			if rec.Task.State != Running {
				continue
			}
			rec.Task.OutcomeUncertain = true
			reason := "Previous process stopped during execution. Detected at startup; actual interruption time and backend outcome are unknown. No backend failure cause is known. Inspect resources before releasing the lock."
			finish(&rec, Interrupted, reason)
			rec.Task.Error = &Diagnostic{Message: reason, Operation: "process interruption"}
			store.Records[id] = rec
		}
		return nil
	}); err != nil {
		return err
	}
	s.started = true
	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				rec, ok := s.claim()
				if ok {
					s.execute(ctx, rec)
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-s.wake:
				case <-ticker.C:
				}
			}
		}()
	}
	return nil
}
func (s *Service) Wait() { s.wg.Wait() }
func (s *Service) claim() (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poison != nil {
		return Record{}, false
	}
	store, err := s.repo.Read()
	if err != nil {
		s.poison = fmt.Errorf("%w: %w", ErrStorageUnavailable, err)
		s.logger.Error().Msg("task storage unavailable; executor fenced")
		return Record{}, false
	}
	var next *Record
	activeGroups := map[string]bool{}
	for _, rec := range store.Records {
		if rec.Task.State == Running {
			for _, group := range rec.ExecutionGroups {
				activeGroups[group] = true
			}
		}
	}
	for _, rec := range store.Records {
		blocked := false
		for _, group := range rec.ExecutionGroups {
			blocked = blocked || activeGroups[group]
		}
		if blocked {
			continue
		}
		if rec.Task.State == Queued && (next == nil || cursor(rec.Task) < cursor(next.Task)) {
			r := rec
			next = &r
		}
	}
	if next == nil {
		return Record{}, false
	}
	err = s.change(next.Task.ID, func(rec *Record) error {
		now := time.Now().UTC()
		rec.Task.State, rec.Task.StartedAt = Running, &now
		rec.Task.Transitions = append(rec.Task.Transitions, Transition{State: Running, At: now})
		*next = *rec
		return nil
	})
	return *next, err == nil
}

func (s *Service) execute(ctx context.Context, rec Record) {
	e := &Execution{ctx: ctx, service: s, id: rec.Task.ID, acceptedAt: rec.Task.CreatedAt, secrets: PayloadSecrets(rec.Payload)}
	err := func() (err error) {
		defer func() {
			if recover() != nil {
				err = errors.New("executor panicked; inspect resource state before retrying")
			}
		}()
		return s.handler.Execute(ctx, Command{Kind: rec.Task.Kind, Payload: rec.Payload, Resources: rec.Task.Resources}, e)
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.persistenceFailed {
		return
	}
	if saveErr := s.change(rec.Task.ID, func(r *Record) error {
		if ctx.Err() != nil {
			r.Task.OutcomeUncertain = true
			r.Task.Error = &Diagnostic{Message: "Process is stopping; backend outcome may be unknown. No automatic retry will occur.", Operation: "process interruption"}
			finish(r, Interrupted, r.Task.Error.Message)
		} else if err != nil {
			r.Task.Error = e.diagnostic(err)
			for i := len(r.Task.Stages) - 1; i >= 0; i-- {
				if r.Task.Stages[i].Error != nil {
					r.Task.Error = r.Task.Stages[i].Error
					break
				}
			}
			r.Task.OutcomeUncertain = e.ambiguous || e.mutating
			finish(r, Failed, "Execution failed; see persisted stages and effects.")
		} else {
			finish(r, Succeeded, "Command completed.")
		}
		return nil
	}); saveErr != nil {
		s.logger.Error().Str("task_id", rec.Task.ID).Msg("cannot persist task outcome; locks retained")
		return
	}
	s.logger.Info().Str("task_id", rec.Task.ID).Bool("failed", err != nil).Msg("task execution finished")
}

// Cleanup is disabled when retention is zero. Unresolved resource locks are
// never removed by retention. Deduplication keys expire with their history.
func (s *Service) Cleanup(now time.Time, retention time.Duration) error {
	if retention <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.poison != nil {
		return s.poison
	}
	err := s.repo.Update(func(store *Store) error {
		for id, rec := range store.Records {
			if rec.Task.State.Terminal() && !rec.Task.BlocksResources && rec.Task.FinishedAt != nil && now.Sub(*rec.Task.FinishedAt) > retention {
				delete(store.Records, id)
			}
		}
		return nil
	})
	if err != nil {
		s.poison = fmt.Errorf("%w; history cleanup failed; restart required: %w", ErrStorageUnavailable, err)
	}
	return err
}
