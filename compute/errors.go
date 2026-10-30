package compute

import "errors"

// ValidationError carries a safe correction hint, separate from backend errors.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return ErrInvalid }

var (
	ErrNotFound    = errors.New("resource not found")
	ErrConflict    = errors.New("resource conflict")
	ErrInvalid     = errors.New("invalid request")
	ErrUnavailable = errors.New("backend unavailable")
)

// ProvisioningError describes confirmed side effects. A failed backend call can
// have an ambiguous outcome; callers must reconcile before retrying mutations.
type ProvisioningError struct {
	Stage     string
	Node      string
	VM        string
	VMCreated bool
	Volumes   []string
	Cause     error
}

func (e *ProvisioningError) Error() string { return "provisioning failed at " + e.Stage }
func (e *ProvisioningError) Unwrap() error { return e.Cause }
