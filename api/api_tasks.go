package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"

	"github.com/danielgtaylor/huma/v2"
)

type SubmissionInput struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"200" doc:"Reuse the same key and equivalent command to recover a lost response. Keys are scoped to the authenticated principal and retained with task history."`
}
type apiTaskInput struct {
	Task string `path:"task" format:"uuid"`
}
type apiTaskListInput struct {
	State         string `query:"state" doc:"Comma-separated lifecycle states."`
	ResourceKind  string `query:"resource_kind" enum:"machine,volume,cloud,machine-request"`
	ResourceScope string `query:"resource_scope"`
	ResourceID    string `query:"resource_id" doc:"Machine identifier or backend volume path, matching task resource references."`
	Blocking      bool   `query:"blocking"`
	Before        string `query:"before" maxLength:"100" doc:"Opaque next cursor from the preceding page."`
	Limit         int    `query:"limit" default:"25" minimum:"1" maximum:"100"`
}
type apiTaskReference struct {
	Task      tasks.Task `json:"task"`
	StatusURL string     `json:"status_url"`
}
type apiAccepted struct {
	Location string `header:"Location"`
	Body     apiTaskReference
}

func taskIdentity(ctx context.Context) tasks.Identity {
	c := claims(ctx)
	name := c.Principal.Name
	if name == "" {
		name = c.Principal.LocalID
	}
	if name == "" {
		name = c.Principal.Email
	}
	return tasks.Identity{ID: c.Principal.ID(), Name: name}
}
func taskError(err error) error {
	if err == nil {
		return nil
	}
	var conflict *tasks.ConflictError
	if errors.As(err, &conflict) {
		return &apiProblem{ErrorModel: huma.ErrorModel{Type: "about:blank", Title: "Resource busy", Status: 409, Detail: "A task holds a conflicting resource lock. Inspect that task before submitting another command."}, Code: "resource_busy", TaskID: conflict.TaskID, TaskURL: "/api/v1/tasks/" + conflict.TaskID}
	}
	switch {
	case errors.Is(err, tasks.ErrStorageUnavailable):
		return &apiProblem{ErrorModel: huma.ErrorModel{Type: "about:blank", Title: "Task storage unavailable", Status: 503, Detail: "Task status or durable submission is temporarily unavailable. Retain the same idempotency key when recovering a submission."}, Code: "task_store_unavailable"}
	case errors.Is(err, tasks.ErrNotFound):
		return huma.Error404NotFound("Task not found.")
	case errors.Is(err, tasks.ErrKeyReuse):
		return &apiProblem{ErrorModel: huma.ErrorModel{Type: "about:blank", Title: "Idempotency key conflict", Status: 409, Detail: "This idempotency key belongs to a different command."}, Code: "idempotency_conflict"}
	case errors.Is(err, tasks.ErrNotQueued):
		return huma.Error409Conflict("Only a task that has not started may be cancelled.")
	case errors.Is(err, tasks.ErrInvalid):
		return huma.Error422UnprocessableEntity("Invalid task request or idempotency key.")
	default:
		return apiError(err)
	}
}
func (e *Environ) submitTask(ctx context.Context, key, kind string, c compute.InfrastructureCommand) (*apiAccepted, error) {
	task, err := e.tasks.Submit(taskIdentity(ctx), key, compute.NewCommand(kind, c))
	if err != nil {
		return nil, taskError(err)
	}
	url := "/api/v1/tasks/" + task.ID
	return &apiAccepted{Location: url, Body: apiTaskReference{Task: task, StatusURL: url}}, nil
}

func (e *Environ) registerTasks(api huma.API) {
	registerAPI(api, http.MethodGet, "/tasks", "list-tasks", "List durable task history", func(ctx context.Context, in *apiTaskListInput) (*apiResult[tasks.Page], error) {
		filter := tasks.Filter{Before: in.Before, Limit: in.Limit, Blocking: in.Blocking}
		if in.State != "" {
			for _, s := range strings.Split(in.State, ",") {
				state := tasks.State(s)
				if state != tasks.Queued && state != tasks.Running && !state.Terminal() {
					return nil, huma.Error422UnprocessableEntity("Unknown task state.")
				}
				filter.States = append(filter.States, state)
			}
		}
		if in.ResourceKind != "" || in.ResourceScope != "" || in.ResourceID != "" {
			filter.Resource = &tasks.ResourceRef{Kind: in.ResourceKind, Scope: in.ResourceScope, ID: in.ResourceID}
		}
		page, err := e.tasks.List(filter)
		return &apiResult[tasks.Page]{Body: page}, taskError(err)
	})
	registerAPI(api, http.MethodGet, "/tasks/{task}", "get-task", "Inspect a durable task and execution timeline", func(ctx context.Context, in *apiTaskInput) (*apiResult[tasks.Task], error) {
		task, err := e.tasks.Get(in.Task)
		return &apiResult[tasks.Task]{Body: task}, taskError(err)
	})
	registerAPI(api, http.MethodPost, "/tasks/{task}/cancel", "cancel-task", "Cancel a queued task before execution", func(ctx context.Context, in *apiTaskInput) (*apiResult[tasks.Task], error) {
		task, err := e.tasks.Cancel(in.Task)
		return &apiResult[tasks.Task]{Body: task}, taskError(err)
	})
	type releaseInput struct {
		Task string `path:"task" format:"uuid"`
		Body struct {
			Reason string `json:"reason" minLength:"3" maxLength:"500" doc:"Operator confirmation after manually inspecting backend resources; never include secrets."`
		}
	}
	registerAPI(api, http.MethodPost, "/tasks/{task}/release", "release-task-locks", "Release uncertain resource locks after manual inspection", func(ctx context.Context, in *releaseInput) (*apiResult[tasks.Task], error) {
		task, err := e.tasks.Release(in.Task, taskIdentity(ctx), in.Body.Reason)
		return &apiResult[tasks.Task]{Body: task}, taskError(err)
	})
}
