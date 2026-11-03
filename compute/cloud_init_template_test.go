package compute

import (
	"errors"
	"strings"
	"testing"
)

type templateTestRepository struct{ entries map[string]CloudInitTemplate }

func (r *templateTestRepository) List() ([]CloudInitTemplate, error) {
	var out []CloudInitTemplate
	for _, item := range r.entries {
		out = append(out, item)
	}
	return out, nil
}
func (r *templateTestRepository) Get(id string) (CloudInitTemplate, error) {
	if item, ok := r.entries[id]; ok {
		return item, nil
	}
	return CloudInitTemplate{}, ErrNotFound
}
func (r *templateTestRepository) Create(item CloudInitTemplate) error {
	r.entries[item.ID] = item
	return nil
}
func (r *templateTestRepository) Update(item CloudInitTemplate, revision string) error {
	previous, err := r.Get(item.ID)
	if err != nil {
		return err
	}
	if previous.Revision != revision {
		return ErrConflict
	}
	r.entries[item.ID] = item
	return nil
}
func (r *templateTestRepository) Delete(id, revision string) error {
	previous, err := r.Get(id)
	if err != nil {
		return err
	}
	if previous.Revision != revision {
		return ErrConflict
	}
	delete(r.entries, id)
	return nil
}

func TestCloudInitTemplateServiceLifecycle(t *testing.T) {
	service := NewCloudInitTemplateService(&templateTestRepository{entries: map[string]CloudInitTemplate{}})
	input := CloudInitTemplateInput{Name: " Bootstrap ", Description: "Guest setup", UserData: "#!/bin/sh\r\nprintf 'ready\\n'\r\n\n"}
	created, err := service.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Bootstrap" || created.UserData != input.UserData || created.ID == created.Revision {
		t.Fatalf("unexpected template: %+v", created)
	}
	ref := CloudInitTemplateRef{ID: created.ID, Revision: created.Revision}
	accepted, err := service.Resolve(ref)
	if err != nil || accepted != input.UserData {
		t.Fatal("user-data was not preserved", err)
	}
	input.Name = "Renamed"
	input.UserData = "#cloud-config\npackages: [curl]\n"
	updated, err := service.Update(created.ID, created.Revision, input)
	if err != nil || updated.ID != created.ID || updated.Revision == created.Revision {
		t.Fatal("revision update failed", err)
	}
	if _, err := service.Resolve(ref); !errors.Is(err, ErrConflict) {
		t.Fatal("stale reference accepted", err)
	}
	if _, err := service.Update(created.ID, created.Revision, input); !errors.Is(err, ErrConflict) {
		t.Fatal("stale update accepted", err)
	}
	if err := service.Delete(created.ID, created.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale delete accepted", err)
	}
	if err := service.Delete(updated.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(CloudInitTemplateRef{ID: updated.ID, Revision: updated.Revision}); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted template resolved", err)
	}
	if accepted != "#!/bin/sh\r\nprintf 'ready\\n'\r\n\n" {
		t.Fatal("resolved snapshot changed")
	}
	list, err := service.List()
	if err != nil || list == nil || len(list) != 0 {
		t.Fatal("empty list", list, err)
	}
}

func TestCloudInitTemplateValidation(t *testing.T) {
	service := NewCloudInitTemplateService(&templateTestRepository{entries: map[string]CloudInitTemplate{}})
	for _, tc := range []struct {
		name  string
		input CloudInitTemplateInput
		field string
	}{
		{"empty name", CloudInitTemplateInput{UserData: "secret"}, "name"},
		{"long name", CloudInitTemplateInput{Name: strings.Repeat("n", 129), UserData: "secret"}, "name"},
		{"multiline name", CloudInitTemplateInput{Name: "one\ntwo", UserData: "secret"}, "name"},
		{"long description", CloudInitTemplateInput{Name: "test", Description: strings.Repeat("d", 1025), UserData: "secret"}, "description"},
		{"empty data", CloudInitTemplateInput{Name: "test", UserData: " \n"}, "user_data"},
		{"long data", CloudInitTemplateInput{Name: "test", UserData: strings.Repeat("s", MaxCloudInitUserDataBytes+1)}, "user_data"},
		{"binary data", CloudInitTemplateInput{Name: "test", UserData: "secret\x00"}, "user_data"},
		{"invalid utf8", CloudInitTemplateInput{Name: "test", UserData: "secret\xff"}, "user_data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Create(tc.input)
			var validation *ValidationError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &validation) || validation.Field != tc.field {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error exposed user-data")
			}
		})
	}
	if err := ValidateCloudInitUserData(""); err != nil {
		t.Fatal("empty machine user-data rejected", err)
	}
	if _, err := service.Create(CloudInitTemplateInput{Name: "Maximum", UserData: strings.Repeat("x", MaxCloudInitUserDataBytes)}); err != nil {
		t.Fatal("maximum payload rejected", err)
	}
	if _, err := service.Get("../../secret"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unsafe ID accepted", err)
	}
	if _, err := service.Resolve(CloudInitTemplateRef{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty ref accepted", err)
	}
}
