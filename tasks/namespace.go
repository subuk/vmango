package tasks

import "strings"

func scopedConflict(store Store, conflict *ConflictError, namespace string) *ConflictError {
	if store.Records[conflict.TaskID].Task.NamespaceID != namespace {
		return &ConflictError{}
	}
	return conflict
}

// Scoped returns a workload view; administration and offline recovery use the
// service directly. Attribution is immutable, so checking before cancel/release
// cannot race with a namespace transfer.
func (s *Service) Scoped(namespace string) *NamespaceService {
	return &NamespaceService{service: s, namespace: namespace}
}

type NamespaceService struct {
	service   *Service
	namespace string
}

func (s *NamespaceService) Get(id string) (Task, error) {
	if s.namespace == "" {
		return Task{}, ErrInvalid
	}
	task, err := s.service.Get(id)
	if err != nil {
		return Task{}, err
	}
	if task.NamespaceID != s.namespace {
		return Task{}, ErrNotFound
	}
	return task, nil
}
func (s *NamespaceService) List(filter Filter) (Page, error) {
	if s.namespace == "" {
		return Page{}, ErrInvalid
	}
	filter.NamespaceID, filter.Scoped = s.namespace, true
	return s.service.List(filter)
}
func (s *NamespaceService) Cancel(id string) (Task, error) {
	if _, err := s.Get(id); err != nil {
		return Task{}, err
	}
	return s.service.Cancel(id)
}
func (s *NamespaceService) Release(id string, who Identity, reason string) (Task, error) {
	if _, err := s.Get(id); err != nil {
		return Task{}, err
	}
	return s.service.Release(id, who, reason)
}

// CheckLocks is used by offline migration while holding the same process lock
// as the server. Existing uncertain or queued work must be recovered first.
func (s *Service) CheckLocks(refs []ResourceRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := s.repo.Read()
	if err != nil {
		return err
	}
	if c := conflict(store, s.lockRefs(refs)); c != nil {
		return c
	}
	return nil
}

func (s *Service) CancelWithReason(id string, who Identity, reason string) (Task, error) {
	if who.ID == "" || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 {
		return Task{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.change(id, func(rec *Record) error {
		if rec.Task.State == Cancelled {
			return nil
		}
		if rec.Task.State != Queued {
			return ErrNotQueued
		}
		finish(rec, Cancelled, "Cancelled by "+who.ID+": "+Redact(reason, nil))
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(id)
}
