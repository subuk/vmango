package api

import (
	"subuk/vmango/tasks"

	"github.com/danielgtaylor/huma/v2"
)

type AdministrationTaskListFields = apiTaskListInput

type apiAdministrationTaskListInput struct {
	AdministrationTaskListFields
	CloudID     string `query:"cloud_id" maxLength:"128"`
	NamespaceID string `query:"namespace_id" maxLength:"128" doc:"Filter persisted task attribution, independently of workload context."`
	Unassigned  bool   `query:"unassigned" doc:"Only tasks without namespace attribution."`
	Host        string `query:"host" maxLength:"128" doc:"Tasks referencing a machine or volume on this host."`
}

func administrationTaskFilter(in *apiAdministrationTaskListInput) (tasks.Filter, error) {
	f, err := taskFilter(&in.AdministrationTaskListFields)
	if err != nil {
		return f, err
	}
	if in.Unassigned && in.NamespaceID != "" {
		return f, huma.Error422UnprocessableEntity("Choose a namespace or unassigned tasks, not both.")
	}
	f.CloudID = in.CloudID
	f.Scoped = in.Unassigned || in.NamespaceID != ""
	f.NamespaceID = in.NamespaceID
	if in.Host != "" {
		if f.Resource != nil {
			return f, huma.Error422UnprocessableEntity("Choose a host or a specific resource filter, not both.")
		}
		f.Resource = &tasks.ResourceRef{Scope: in.Host}
		f.ResourceKinds = []string{"machine", "volume"}
	}
	return f, nil
}
