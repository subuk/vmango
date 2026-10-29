package libvirt

import (
	"errors"
	"fmt"
	vir "libvirt.org/go/libvirt"
	"subuk/vmango/compute"
	"subuk/vmango/util"
)

// Translate backend categories at the repository boundary, preserving the cause
// for diagnostics without exposing backend details in HTTP responses.
func repositoryError(err error, message string, args ...interface{}) error {
	var kind error
	switch {
	case errors.Is(err, vir.ERR_NO_DOMAIN), errors.Is(err, vir.ERR_NO_STORAGE_VOL), errors.Is(err, vir.ERR_NO_STORAGE_POOL), errors.Is(err, vir.ERR_NO_NETWORK):
		kind = compute.ErrNotFound
	case errors.Is(err, vir.ERR_OPERATION_INVALID), errors.Is(err, vir.ERR_OPERATION_DENIED), errors.Is(err, vir.ERR_RESOURCE_BUSY), errors.Is(err, vir.ERR_OPERATION_FAILED):
		kind = compute.ErrConflict
	case errors.Is(err, vir.ERR_INVALID_ARG), errors.Is(err, vir.ERR_CONFIG_UNSUPPORTED), errors.Is(err, vir.ERR_XML_ERROR):
		kind = compute.ErrInvalid
	case errors.Is(err, vir.ERR_NO_CONNECT), errors.Is(err, vir.ERR_SYSTEM_ERROR), errors.Is(err, vir.ERR_RPC), errors.Is(err, vir.ERR_OPERATION_TIMEOUT):
		kind = compute.ErrUnavailable
	}
	if kind != nil {
		err = fmt.Errorf("%w: %w", kind, err)
	}
	return util.NewError(err, message, args...)
}
