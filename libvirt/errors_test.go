package libvirt

import (
	"errors"
	vir "libvirt.org/go/libvirt"
	"subuk/vmango/compute"
	"testing"
)

func TestRepositoryErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		code vir.ErrorNumber
		want error
	}{{vir.ERR_NO_DOMAIN, compute.ErrNotFound}, {vir.ERR_NO_STORAGE_VOL, compute.ErrNotFound}, {vir.ERR_OPERATION_INVALID, compute.ErrConflict}, {vir.ERR_INVALID_ARG, compute.ErrInvalid}, {vir.ERR_NO_CONNECT, compute.ErrUnavailable}} {
		err := repositoryError(vir.Error{Code: tc.code, Message: "backend detail"}, "operation failed")
		if !errors.Is(err, tc.want) {
			t.Fatalf("%v: %v", tc.code, err)
		}
		if !errors.Is(err, tc.code) {
			t.Fatal("backend cause lost")
		}
	}
}
