package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

func TestConsoleUnavailableOnlyExposesSafeRecoveryHint(t *testing.T) {
	safe := "Graphical console unavailable: verify SSH access, non-interactive authentication and trusted host keys. VM management remains available."
	err := fmt.Errorf("SSH key /private/credential: %w", &compute.ConsoleUnavailableError{Message: safe})
	if !errors.Is(err, compute.ErrUnavailable) {
		t.Fatal("console failure must retain backend-unavailable classification")
	}
	problem, ok := apiError(err).(*huma.ErrorModel)
	if !ok || problem.Status != 503 || problem.Detail != safe {
		t.Fatalf("console recovery hint lost or private diagnostics exposed: %+v", problem)
	}
	unknown, ok := apiError(fmt.Errorf("SSH key /private/credential: %w", compute.ErrUnavailable)).(*huma.ErrorModel)
	if !ok || unknown.Status != 503 || unknown.Detail != "Backend unavailable." {
		t.Fatalf("untyped backend error must stay sanitized: %+v", unknown)
	}
}
