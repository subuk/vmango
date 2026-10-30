// Package tasks owns durable command submission and execution history.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type State string

const (
	Queued      State = "queued"
	Running     State = "running"
	Succeeded   State = "succeeded"
	Failed      State = "failed"
	Cancelled   State = "cancelled"
	Interrupted State = "interrupted"
)

func (s State) Terminal() bool {
	return s == Succeeded || s == Failed || s == Cancelled || s == Interrupted
}

// ResourceRef is provider independent. Scope identifies a configured backend,
// while ID is a stable backend resource identifier, not a display label.
type ResourceRef struct {
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	ID       string `json:"id"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type Identity struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type Diagnostic struct {
	Message   string       `json:"message"`
	Backend   string       `json:"backend,omitempty"`
	Operation string       `json:"operation,omitempty"`
	Code      string       `json:"code,omitempty"`
	Domain    string       `json:"domain,omitempty"`
	Resource  *ResourceRef `json:"resource,omitempty"`
}

// DiagnosticError preserves adapter diagnostics through service wrappers.
type DiagnosticError struct {
	Details Diagnostic
	Cause   error
	Effects []Effect
}

func (e *DiagnosticError) Error() string { return e.Details.Message }
func (e *DiagnosticError) Unwrap() error { return e.Cause }

type Effect struct {
	Resource ResourceRef `json:"resource"`
	Action   string      `json:"action"`
	At       time.Time   `json:"at"`
}

type Stage struct {
	Name       string            `json:"name"`
	Resource   *ResourceRef      `json:"resource,omitempty"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	State      string            `json:"state"`
	Error      *Diagnostic       `json:"error,omitempty"`
	Transfer   *TransferProgress `json:"transfer,omitempty"`
}

// TransferProgress measures bytes forwarded to the destination, including
// backpressure. Estimates describe transfer time only, not VM provisioning.
type TransferProgress struct {
	Bytes            uint64    `json:"bytes"`
	TotalBytes       uint64    `json:"total_bytes,omitempty"`
	BytesPerSecond   float64   `json:"bytes_per_second"`
	RemainingSeconds *uint64   `json:"remaining_seconds,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Transition struct {
	State  State     `json:"state"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason,omitempty"`
}

type Release struct {
	By     Identity  `json:"by"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
}

type Task struct {
	ID               string        `json:"id"`
	Kind             string        `json:"kind"`
	State            State         `json:"state"`
	Initiator        Identity      `json:"initiator"`
	CreatedAt        time.Time     `json:"created_at"`
	StartedAt        *time.Time    `json:"started_at,omitempty"`
	FinishedAt       *time.Time    `json:"finished_at,omitempty"`
	Resources        []ResourceRef `json:"resources"`
	Stages           []Stage       `json:"stages"`
	Transitions      []Transition  `json:"transitions"`
	Effects          []Effect      `json:"effects"`
	Error            *Diagnostic   `json:"error,omitempty"`
	OutcomeUncertain bool          `json:"outcome_uncertain"`
	BlocksResources  bool          `json:"blocks_resources"`
	LockRelease      *Release      `json:"lock_release,omitempty"`
}

// Record is private storage data. Only Task may cross the public API boundary.
type Record struct {
	Task            Task            `json:"task"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	KeyHash         string          `json:"key_hash"`
	Fingerprint     string          `json:"fingerprint"`
	Locks           []ResourceRef   `json:"locks"`
	ExecutionGroups []string        `json:"execution_groups,omitempty"`
}

type Store struct {
	Version int               `json:"version"`
	Records map[string]Record `json:"records"`
}

// Repository commits the entire callback atomically and durably. A failed
// commit must never permit backend execution or release resource locks.
type Repository interface {
	Read() (Store, error)
	Update(func(*Store) error) error
}

type Command struct {
	Kind      string
	Payload   json.RawMessage
	Resources []ResourceRef
	// ExecutionGroups serialize accepted commands without occupying a worker
	// while another member runs. They are scheduling hints, not dependencies.
	ExecutionGroups []string
}

type Handler interface {
	// Prepare validates and snapshots a command before acceptance, without writes
	// to infrastructure. Submission holds the coordination lock while planning.
	Prepare(Command) (Command, error)
	Execute(context.Context, Command, *Execution) error
}

// LockResolver canonicalizes backend aliases without changing public references.
type LockResolver interface {
	LockRefs([]ResourceRef) []ResourceRef
}

type Filter struct {
	States   []State
	Resource *ResourceRef
	Blocking bool
	Before   string
	Limit    int
}
type Page struct {
	Items  []Task `json:"items"`
	Next   string `json:"next,omitempty"`
	Active int    `json:"active"`
}

var (
	ErrNotFound           = errors.New("task not found")
	ErrInvalid            = errors.New("invalid task request")
	ErrKeyReuse           = errors.New("idempotency key reused for a different command")
	ErrNotQueued          = errors.New("only queued tasks may be cancelled")
	ErrStorageUnavailable = errors.New("task storage unavailable")
)

type ConflictError struct{ TaskID string }

func (e *ConflictError) Error() string { return "resource is blocked by task " + e.TaskID }
