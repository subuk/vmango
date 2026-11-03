package compute_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
)

func templateFixture(t *testing.T) *compute.CloudInitTemplateService {
	t.Helper()
	repo, err := filesystem.NewCloudInitTemplateRepository(filepath.Join(t.TempDir(), "templates"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	return compute.NewCloudInitTemplateService(repo)
}

func TestCloudCreatePinsTemplateBeforeAcceptanceAndRestart(t *testing.T) {
	handler, _, _, _ := cloudFixtureInitialization(t, 4, nil, "nocloud")
	templates := templateFixture(t)
	handler.CloudInitTemplates = templates
	original := "#cloud-config\npassword: private-bootstrap-secret\nwrite_files:\n  - path: /etc/example\n    content: |\n      preserve whitespace\n\n"
	saved, err := templates.Create(compute.CloudInitTemplateInput{Name: "Provisioning", UserData: original})
	if err != nil {
		t.Fatal(err)
	}
	spec := createSpec("pinned")
	spec.UserDataTemplate = &compute.CloudInitTemplateRef{ID: saved.ID, Revision: saved.Revision}
	command := compute.NewCommand("machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec})
	dir := filepath.Join(t.TempDir(), "tasks")
	repo, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { repo.Close() }()
	service := tasks.New(repo, handler, zerolog.Nop())
	identity := tasks.Identity{ID: "admin"}
	accepted, err := service.Submit(identity, "template-snapshot", command)
	if err != nil {
		t.Fatal(err)
	}
	// A later edit and deletion cannot alter the accepted content or deduplication.
	edited, err := templates.Update(saved.ID, saved.Revision, compute.CloudInitTemplateInput{Name: "Edited", UserData: "#!/bin/sh\necho changed\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err = templates.Delete(edited.ID, edited.Revision); err != nil {
		t.Fatal(err)
	}
	if err = repo.Close(); err != nil {
		t.Fatal(err)
	}
	repo, err = filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	service = tasks.New(repo, handler, zerolog.Nop())
	duplicate, err := service.Submit(identity, "template-snapshot", command)
	if err != nil || duplicate.ID != accepted.ID {
		t.Fatalf("replay after deletion: %v %+v", err, duplicate)
	}
	state, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	record := state.Records[accepted.ID]
	var snapshot compute.InfrastructureCommand
	if err = json.Unmarshal(record.Payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.CreateSpec.UserDataTemplate != nil || snapshot.CreateSpec.UserData != original {
		t.Fatal("accepted task retained a mutable reference or changed user-data")
	}
	encoded, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("private-bootstrap-secret")) {
		t.Fatal("task response exposed user-data")
	}
	redacted := tasks.Redact("backend failed with "+original, tasks.PayloadSecrets(record.Payload))
	if strings.Contains(redacted, "private-bootstrap-secret") {
		t.Fatal("accepted user-data escaped diagnostic redaction")
	}
	if _, err = service.Cancel(accepted.ID); err != nil {
		t.Fatal(err)
	}
	state, err = repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Submit(identity, "deleted-template", command); !errors.Is(err, compute.ErrNotFound) {
		t.Fatalf("new request should reject deleted template: %v", err)
	}
	if len(state.Records[accepted.ID].Payload) != 0 {
		t.Fatal("terminal task retained private content")
	}
}

func TestCloudCreateTemplateValidation(t *testing.T) {
	for _, initialization := range []string{"nocloud", "none"} {
		t.Run(initialization, func(t *testing.T) {
			handler, _, _, cloud := cloudFixtureInitialization(t, 4, nil, initialization)
			templates := templateFixture(t)
			handler.CloudInitTemplates = templates
			saved, err := templates.Create(compute.CloudInitTemplateInput{Name: "Setup", UserData: "#cloud-config\npackages: [curl]\n"})
			if err != nil {
				t.Fatal(err)
			}
			spec := createSpec("new")
			spec.UserDataTemplate = &compute.CloudInitTemplateRef{ID: saved.ID, Revision: saved.Revision}
			prepare := func() error {
				_, err := handler.Prepare(compute.NewCommand("machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec}))
				return err
			}
			if initialization == "none" {
				if err := prepare(); !errors.Is(err, compute.ErrInvalid) {
					t.Fatalf("disabled initialization accepted template: %v", err)
				}
				spec.UserDataTemplate = nil
				spec.UserData = saved.UserData
				if err := handler.Clouds.ValidateCreate(cloud, *spec); !errors.Is(err, compute.ErrInvalid) {
					t.Fatalf("disabled initialization accepted inline data: %v", err)
				}
				return
			}
			spec.UserData = "#!/bin/sh\necho inline"
			if err := prepare(); !errors.Is(err, compute.ErrInvalid) {
				t.Fatalf("mixed sources accepted: %v", err)
			}
			spec.UserData = ""
			if _, err := templates.Update(saved.ID, saved.Revision, compute.CloudInitTemplateInput{Name: "Setup", UserData: "#cloud-config\npackages: [git]\n"}); err != nil {
				t.Fatal(err)
			}
			if err := prepare(); !errors.Is(err, compute.ErrConflict) {
				t.Fatalf("stale preview accepted: %v", err)
			}
			spec.UserDataTemplate = nil
			spec.UserData = saved.UserData
			prepared, err := handler.Prepare(compute.NewCommand("machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec}))
			if err != nil {
				t.Fatal(err)
			}
			var snapshot compute.InfrastructureCommand
			if err := json.Unmarshal(prepared.Payload, &snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.CreateSpec.UserData != saved.UserData {
				t.Fatal("legacy inline user-data changed")
			}
		})
	}
}
