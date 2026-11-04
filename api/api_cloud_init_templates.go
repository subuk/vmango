package api

import (
	"context"
	"errors"
	"subuk/vmango/compute"

	"github.com/danielgtaylor/huma/v2"
)

type APICloudInitTemplateSummary struct {
	NamespaceID compute.NamespaceID `json:"namespace_id,omitempty"`
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Revision    string              `json:"revision"`
}

type apiCloudInitTemplate struct {
	APICloudInitTemplateSummary
	UserData string `json:"user_data"`
}

type APICloudInitTemplateInput struct {
	Name        string `json:"name" minLength:"1" maxLength:"128"`
	Description string `json:"description,omitempty" maxLength:"1024"`
	UserData    string `json:"user_data" minLength:"1" maxLength:"262144"`
}

func (in APICloudInitTemplateInput) domain() compute.CloudInitTemplateInput {
	return compute.CloudInitTemplateInput{Name: in.Name, Description: in.Description, UserData: in.UserData}
}

func cloudInitTemplateSummary(t compute.CloudInitTemplate) APICloudInitTemplateSummary {
	return APICloudInitTemplateSummary{NamespaceID: t.NamespaceID, ID: t.ID, Name: t.Name, Description: t.Description, Revision: t.Revision}
}

func cloudInitTemplateDTO(t compute.CloudInitTemplate) apiCloudInitTemplate {
	return apiCloudInitTemplate{APICloudInitTemplateSummary: cloudInitTemplateSummary(t), UserData: t.UserData}
}

func templateAPIError(err error) error {
	if errors.Is(err, compute.ErrConflict) {
		return huma.Error409Conflict("The template changed. Reload it and review the current contents before trying again.")
	}
	return apiError(err)
}

func (e *Environ) registerCloudInitTemplates(api huma.API) {
	const path = "/cloud-init-templates"
	type itemInput struct {
		ID string `path:"id" format:"uuid"`
	}
	type createInput struct {
		Body APICloudInitTemplateInput
	}
	type updateInput struct {
		ID   string `path:"id" format:"uuid"`
		Body struct {
			APICloudInitTemplateInput
			Revision string `json:"revision" format:"uuid"`
		}
	}
	type deleteInput struct {
		ID   string `path:"id" format:"uuid"`
		Body struct {
			Revision string `json:"revision" format:"uuid"`
		}
	}
	registerAPI(api, "GET", path, "list-cloud-init-templates", "List saved cloud-init templates without user-data", func(ctx context.Context, in *apiEmpty) (*apiResult[[]APICloudInitTemplateSummary], error) {
		templates, scopeErr := e.cloudInitTemplates.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		if e.cloudInitTemplates == nil {
			return nil, apiError(compute.ErrUnavailable)
		}
		items, err := templates.List()
		if err != nil {
			return nil, templateAPIError(err)
		}
		out := make([]APICloudInitTemplateSummary, 0, len(items))
		for _, item := range items {
			out = append(out, cloudInitTemplateSummary(item))
		}
		return &apiResult[[]APICloudInitTemplateSummary]{Body: out}, nil
	})
	registerAPI(api, "POST", path, "create-cloud-init-template", "Save a cloud-init template", func(ctx context.Context, in *createInput) (*apiResult[apiCloudInitTemplate], error) {
		templates, scopeErr := e.cloudInitTemplates.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		if e.cloudInitTemplates == nil {
			return nil, apiError(compute.ErrUnavailable)
		}
		item, err := templates.Create(in.Body.domain())
		if err != nil {
			return nil, templateAPIError(err)
		}
		return &apiResult[apiCloudInitTemplate]{Body: cloudInitTemplateDTO(item)}, nil
	})
	registerAPI(api, "GET", path+"/{id}", "get-cloud-init-template", "Read a cloud-init template and its revision", func(ctx context.Context, in *itemInput) (*apiResult[apiCloudInitTemplate], error) {
		templates, scopeErr := e.cloudInitTemplates.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		if e.cloudInitTemplates == nil {
			return nil, apiError(compute.ErrUnavailable)
		}
		item, err := templates.Get(in.ID)
		if err != nil {
			return nil, templateAPIError(err)
		}
		return &apiResult[apiCloudInitTemplate]{Body: cloudInitTemplateDTO(item)}, nil
	})
	registerAPI(api, "PUT", path+"/{id}", "update-cloud-init-template", "Replace a cloud-init template at the reviewed revision", func(ctx context.Context, in *updateInput) (*apiResult[apiCloudInitTemplate], error) {
		templates, scopeErr := e.cloudInitTemplates.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		if e.cloudInitTemplates == nil {
			return nil, apiError(compute.ErrUnavailable)
		}
		item, err := templates.Update(in.ID, in.Body.Revision, in.Body.APICloudInitTemplateInput.domain())
		if err != nil {
			return nil, templateAPIError(err)
		}
		return &apiResult[apiCloudInitTemplate]{Body: cloudInitTemplateDTO(item)}, nil
	})
	registerAPI(api, "DELETE", path+"/{id}", "delete-cloud-init-template", "Delete a cloud-init template at the reviewed revision", func(ctx context.Context, in *deleteInput) (*apiEmpty, error) {
		templates, scopeErr := e.cloudInitTemplates.ForNamespace(requestNamespace(ctx))
		if scopeErr != nil {
			return nil, apiError(scopeErr)
		}
		if e.cloudInitTemplates == nil {
			return nil, apiError(compute.ErrUnavailable)
		}
		return nil, templateAPIError(templates.Delete(in.ID, in.Body.Revision))
	})
}
