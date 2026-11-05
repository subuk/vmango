package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateBackupRejectsUnsafeACL(t *testing.T) {
	for _, directory := range []bool{true, false} {
		t.Run(map[bool]string{true: "directory", false: "file"}[directory], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "backup")
			if err := CreatePrivateBackupDirectory(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "manifest.json")
			if err := WritePrivateBackupFile(path, []byte("original")); err != nil {
				t.Fatal(err)
			}
			unsafePath := path
			if directory {
				unsafePath = dir
			}
			sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
			if err != nil {
				t.Fatal(err)
			}
			dacl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetNamedSecurityInfo(unsafePath, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
				t.Fatal(err)
			}
			if err := CheckPrivateBackupPath(unsafePath, directory); err == nil {
				t.Fatal("backup accessible to other accounts accepted")
			}
			if err := WritePrivateBackupFile(path, []byte("replacement")); err == nil {
				t.Fatal("unsafe backup overwritten")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "original" {
				t.Fatal("rejected write changed contents", err)
			}
		})
	}
}
