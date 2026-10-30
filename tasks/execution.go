package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

type Execution struct {
	ctx               context.Context
	service           *Service
	id                string
	acceptedAt        time.Time
	secrets           []string
	mutating          bool
	ambiguous         bool
	persistenceFailed bool
}

func (e *Execution) ID() string { return e.id }

// Protect durably acquires concrete resource locks after scheduling, before
// provisioning. A failed lock/history write must prevent all backend mutations.
func (e *Execution) Protect(refs []ResourceRef) error {
	e.service.mu.Lock()
	defer e.service.mu.Unlock()
	store, err := e.service.repo.Read()
	if err != nil {
		e.persistenceFailed = true
		return err
	}
	delete(store.Records, e.id)
	locks := e.service.lockRefs(refs)
	if c := conflict(store, locks); c != nil {
		return c
	}
	err = e.service.change(e.id, func(rec *Record) error {
		rec.Task.Resources = unique(append(rec.Task.Resources, refs...))
		rec.Locks = unique(append(rec.Locks, locks...))
		return nil
	})
	if err != nil {
		e.persistenceFailed = true
	}
	return err
}

func (e *Execution) AcceptedAt() time.Time { return e.acceptedAt }

// Transfer updates the currently running stage. Callers throttle persistence
// to at most once per second, with an additional final sample.
func (e *Execution) Transfer(p TransferProgress) error {
	return e.save(func(rec *Record) error {
		if n := len(rec.Task.Stages); n > 0 {
			rec.Task.Stages[n-1].Transfer = &p
		}
		return nil
	})
}

func (e *Execution) save(fn func(*Record) error) error {
	e.service.mu.Lock()
	defer e.service.mu.Unlock()
	err := e.service.change(e.id, fn)
	if err != nil {
		e.persistenceFailed = true
	}
	return err
}

// Stage persists intent before invoking the operation. An unrecorded stage may
// never execute; a failed history write fences further work until restart.
func (e *Execution) Stage(name string, resource *ResourceRef, mutates bool, run func() error) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	index := 0
	if err := e.save(func(rec *Record) error {
		index = len(rec.Task.Stages)
		rec.Task.Stages = append(rec.Task.Stages, Stage{Name: name, Resource: resource, State: "running", StartedAt: time.Now().UTC()})
		return nil
	}); err != nil {
		return err
	}
	e.mutating = mutates
	e.service.logger.Info().Str("task_id", e.id).Str("stage", name).Msg("task stage started")
	err := run()
	if e.persistenceFailed {
		return errors.New("task history unavailable; execution stopped")
	}
	if err != nil {
		e.ambiguous = e.ambiguous || mutates
		e.service.logger.Error().Str("task_id", e.id).Str("stage", name).Interface("diagnostic", e.diagnostic(err)).Msg("task stage failed")
	}
	if saveErr := e.save(func(rec *Record) error {
		stage := &rec.Task.Stages[index]
		now := time.Now().UTC()
		stage.FinishedAt = &now
		stage.State = "succeeded"
		if err != nil {
			stage.State = "failed"
			stage.Error = e.diagnostic(err)
			if stage.Error.Operation == "" {
				stage.Error.Operation = name
			}
			if stage.Error.Resource == nil {
				stage.Error.Resource = resource
			}
		}
		var backend *DiagnosticError
		if errors.As(err, &backend) {
			rec.Task.Effects = append(rec.Task.Effects, backend.Effects...)
		}
		return nil
	}); saveErr != nil {
		return saveErr
	}
	e.mutating = false
	return err
}

// Effect records only a confirmed change, independently of the enclosing stage.
func (e *Execution) Effect(resource ResourceRef, action string) error {
	return e.save(func(rec *Record) error {
		rec.Task.Effects = append(rec.Task.Effects, Effect{resource, action, time.Now().UTC()})
		found := false
		for _, r := range rec.Task.Resources {
			if r.Kind == resource.Kind && r.Scope == resource.Scope && r.ID == resource.ID {
				found = true
			}
		}
		if !found {
			rec.Task.Resources = append(rec.Task.Resources, resource)
		}
		return nil
	})
}

func (e *Execution) diagnostic(err error) *Diagnostic {
	d := Diagnostic{Message: err.Error()}
	var backend *DiagnosticError
	if errors.As(err, &backend) {
		d = backend.Details
	}
	d.Message = Redact(d.Message, e.secrets)
	d.Operation = Redact(d.Operation, e.secrets)
	return &d
}

var sensitiveAssignment = regexp.MustCompile(`(?im)(["']?(?:password|passwd|token|secret|authorization|api[_-]?key|access[_-]?key|client[_-]?secret)["']?\s*[:=]\s*)(?:"[^"\n]*"|'[^'\n]*'|[^\s,;<]+)`)
var bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
var urlCredentials = regexp.MustCompile(`(\w+://)[^\s/@]+:[^\s/@]+@`)
var privateKey = regexp.MustCompile(`(?s)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----`)
var cloudXML = regexp.MustCompile(`(?s)<(?:user_data|userdata|password|secret)(?:\s[^>]*)?>.*?</(?:user_data|userdata|password|secret)>`)

// Redact retains backend causes and QEMU diagnostics, stripping credentials and
// known private command fragments. Arbitrary hook stdout is excluded at source.
func Redact(message string, secrets []string) string {
	for _, secret := range secrets {
		if len(secret) >= 3 {
			message = strings.ReplaceAll(message, secret, "[redacted]")
			escaped, _ := json.Marshal(secret)
			message = strings.ReplaceAll(message, strings.Trim(string(escaped), "\""), "[redacted]")
		}
	}
	message = privateKey.ReplaceAllString(message, "[redacted private key]")
	message = cloudXML.ReplaceAllString(message, "[redacted private data]")
	message = sensitiveAssignment.ReplaceAllString(message, "${1}[redacted]")
	message = bearerPattern.ReplaceAllString(message, "Bearer [redacted]")
	message = urlCredentials.ReplaceAllString(message, "${1}[redacted]@")
	if len(message) > 16384 {
		message = message[:16384] + " [truncated]"
	}
	return message
}

func PayloadSecrets(payload json.RawMessage) []string {
	var data any
	_ = json.Unmarshal(payload, &data)
	secrets := []string{}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for k, child := range v {
				key := strings.ToLower(k)
				if text, ok := child.(string); ok && (strings.Contains(key, "userdata") || strings.Contains(key, "user_data") || strings.Contains(key, "password") || strings.Contains(key, "secret") || strings.Contains(key, "token")) {
					if key == "userdata" {
						if decoded, err := base64.StdEncoding.DecodeString(text); err == nil {
							secrets = append(secrets, text)
							text = string(decoded)
						}
					}
					secrets = append(secrets, text)
					for _, line := range strings.Split(text, "\n") {
						if len(strings.TrimSpace(line)) >= 3 {
							secrets = append(secrets, strings.TrimSpace(line))
						}
					}
					for _, match := range sensitiveAssignment.FindAllString(text, -1) {
						if pos := strings.IndexAny(match, ":="); pos >= 0 {
							secrets = append(secrets, strings.Trim(strings.TrimSpace(match[pos+1:]), "\"'"))
						}
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(data)
	return secrets
}
