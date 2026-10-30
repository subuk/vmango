package filesystem

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

func currentAuthSID() (*windows.SID, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}
func makeAuthDirectory(path string) error {
	_, err := os.Lstat(path)
	missing := errors.Is(err, os.ErrNotExist)
	if err = os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if !missing {
		return nil
	}
	sid, err := currentAuthSID()
	if err != nil {
		return err
	}
	// Protected inheritance grants access only to the service account.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION, sid, nil, dacl, nil)
}
func checkAuthPath(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || dir != info.IsDir() || !dir && !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe authentication store object: %s", path)
	}
	sid, err := currentAuthSID()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(sid) {
		return fmt.Errorf("authentication store must be owned by the service account: %s", path)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount == 0 {
		return fmt.Errorf("authentication store needs a private DACL: %s", path)
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(sid) {
			return fmt.Errorf("authentication store grants access outside the service account: %s", path)
		}
	}
	return nil
}
func openAuthFile(path string, create bool) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access, disposition := uint32(windows.GENERIC_READ), uint32(windows.OPEN_EXISTING)
	if create {
		access |= windows.GENERIC_WRITE
		disposition = windows.OPEN_ALWAYS
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, disposition, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
func lockAuthFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
func replaceAuthFile(old, new string) error {
	a, err := windows.UTF16PtrFromString(old)
	if err != nil {
		return err
	}
	b, err := windows.UTF16PtrFromString(new)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(a, b, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// MoveFileEx with WRITE_THROUGH supplies Windows' supported commit guarantee.
func syncAuthDirectory(string) error { return nil }
