package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"subuk/vmango/compute"
	"testing"
)

func assignmentVolume() *compute.Volume {
	return &compute.Volume{NodeId: "host", Path: "/pool/disk", Name: "backend-name", Pool: "pool", PoolIdentity: "pool-uuid", BackendKey: "key", Identity: "scope\x00pool-uuid\x00key"}
}

func TestVolumeAssignmentsRestartIdentityAndDeletion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "assignments")
	r, err := NewVolumeAssignmentRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	v := assignmentVolume()
	if got, err := r.Get(v, "scope"); err != nil || got != nil {
		t.Fatalf("missing assignment: %+v %v", got, err)
	}
	if other, err := NewVolumeAssignmentRepository(dir); err == nil {
		other.Close()
		t.Fatal("competing writer acquired registry")
	}
	a := compute.VolumeAssignment{NamespaceID: "platform", Label: "Equal display label"}
	if err := r.Put(v, "scope", a); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckCreate(v.NodeId, v.Path, "scope"); !errors.Is(err, compute.ErrConflict) {
		t.Fatal("stale assignment could be inherited by a new volume", err)
	}
	if err := r.Put(v, "scope", a); err != nil {
		t.Fatal("idempotent assignment failed", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewVolumeAssignmentRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.Get(v, "scope"); err != nil || got == nil || *got != a {
		t.Fatalf("assignment lost after restart: %+v %v", got, err)
	}
	for _, change := range []func(*compute.Volume){
		func(v *compute.Volume) { v.Identity = "replaced" },
		func(v *compute.Volume) { v.PoolIdentity = "recreated-pool" },
		func(v *compute.Volume) { v.BackendKey = "replaced-key" },
		func(v *compute.Volume) { v.Pool = "other-pool" },
		func(v *compute.Volume) { v.NodeId = "renamed-host" },
	} {
		changed := *v
		change(&changed)
		if _, err := r.Get(&changed, "scope"); !errors.Is(err, compute.ErrConflict) {
			t.Fatalf("replacement inherited stale assignment: %v", err)
		}
		if err := r.Put(&changed, "scope", a); !errors.Is(err, compute.ErrConflict) {
			t.Fatalf("replacement overwrote old record: %v", err)
		}
		if err := r.Delete(&changed, "scope"); !errors.Is(err, compute.ErrConflict) {
			t.Fatalf("replacement removed old record: %v", err)
		}
	}
	if err := r.Put(v, "scope", compute.VolumeAssignment{NamespaceID: "analytics", Label: a.Label}); !errors.Is(err, compute.ErrConflict) {
		t.Fatal("assignment transferred namespaces", err)
	}
	clone := *v
	clone.Path, clone.Identity, clone.BackendKey = "/pool/clone", "clone-identity", "clone-key"
	if got, err := r.Get(&clone, "scope"); err != nil || got != nil {
		t.Fatal("clone inherited source record")
	}
	if err := r.Put(&clone, "scope", a); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(v, "scope"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(v, "scope"); err != nil {
		t.Fatal("deletion is not idempotent", err)
	}
	if got, err := r.Get(v, "scope"); err != nil || got != nil {
		t.Fatal("deleted volume retained assignment")
	}
	if err := r.CheckCreate(v.NodeId, v.Path, "scope"); err != nil {
		t.Fatal("confirmed deletion did not release assignment address", err)
	}
	if got, err := r.Get(&clone, "scope"); err != nil || got == nil || *got != a {
		t.Fatal("source deletion affected clone")
	}
}

func TestVolumeAssignmentStorageErrorsNeverMeanUnassigned(t *testing.T) {
	for _, body := range []string{"not json", `{"version":2,"records":{}}`, `{"version":1,"records":null}`, `{"version":1,"records":{}} trailing`, `{"version":1,"records":{},"unexpected":true}`} {
		dir := filepath.Join(t.TempDir(), "registry")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "volumes.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if r, err := NewVolumeAssignmentRepository(dir); err == nil {
			r.Close()
			t.Fatalf("corrupt registry accepted: %s", body)
		}
	}
	dir := filepath.Join(t.TempDir(), "assignment")
	r, err := NewVolumeAssignmentRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := os.Rename(dir, dir+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Get(assignmentVolume(), "scope"); err == nil || got != nil {
		t.Fatal("unavailable registry returned empty membership")
	}
}

func TestVolumeAssignmentFailedCommitRequiresRecovery(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "registry")
	r, err := NewVolumeAssignmentRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := assignmentVolume()
	a := compute.VolumeAssignment{NamespaceID: "platform", Label: "label"}
	r.write = func(volumeAssignmentStore) error { return errors.New("injected atomic replacement failure") }
	if err := r.Put(v, "scope", a); err == nil {
		t.Fatal("failed commit reported success")
	}
	if _, err := r.Get(v, "scope"); err == nil {
		t.Fatal("failed commit allowed read success before recovery")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewVolumeAssignmentRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got, err := r.Get(v, "scope"); err != nil || got != nil {
		t.Fatalf("failed write was promoted: %+v %v", got, err)
	}
	if err := r.Put(v, "scope", a); err != nil {
		t.Fatal("recovery could not retry assignment", err)
	}
}
