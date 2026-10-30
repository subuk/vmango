package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAuthFileCreationUsesPrivateServiceOwnership(t *testing.T) {
	t.Logf("elevated process: %t", windows.GetCurrentProcessToken().IsElevated())
	dir := filepath.Join(t.TempDir(), "store")
	if err := makeAuthDirectory(dir); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, ".lock")
	lock, err := openAuthFile(lockPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = checkAuthPath(lockPath, false); err != nil {
		t.Fatalf("new lock owner or DACL: %v", err)
	}
	pending, err := createAuthTemp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	if err = checkAuthPath(pending.Name(), false); err != nil {
		t.Fatalf("temporary file must be private before writing credentials: %v", err)
	}
	if _, err = pending.WriteString("private record"); err != nil {
		t.Fatal(err)
	}
	if err = pending.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = pending.Close(); err != nil {
		t.Fatal(err)
	}
	committed := filepath.Join(dir, "record.json")
	if err = replaceAuthFile(pending.Name(), committed); err != nil {
		t.Fatal(err)
	}
	if err = checkAuthPath(committed, false); err != nil {
		t.Fatalf("committed owner or DACL: %v", err)
	}
	data, err := os.ReadFile(committed)
	if err != nil || string(data) != "private record" {
		t.Fatalf("committed contents: %q, %v", data, err)
	}
}

func TestOpeningExistingAuthFileDoesNotRepairUnsafePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	if err := makeAuthDirectory(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".lock")
	f, err := openAuthFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if err = checkAuthPath(path, false); err == nil {
		t.Fatal("unsafe DACL accepted")
	}
	f, err = openAuthFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = checkAuthPath(path, false); err == nil {
		t.Fatal("opening an existing file silently replaced its unsafe DACL")
	}
}
