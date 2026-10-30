package libvirt

import (
	"errors"
	"fmt"
	vir "libvirt.org/go/libvirt"
	"strconv"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
	"subuk/vmango/util"
	"time"
)

func mutationError(err error, operation string, resource tasks.ResourceRef, effects []tasks.Effect) error {
	if err == nil {
		return nil
	}
	err = repositoryError(err, "%s", operation)
	d := tasks.Diagnostic{Message: err.Error(), Backend: "libvirt", Operation: operation, Resource: &resource}
	var previous *tasks.DiagnosticError
	if errors.As(err, &previous) {
		d = previous.Details
		d.Resource = &resource
		effects = append(effects, previous.Effects...)
	}
	return &tasks.DiagnosticError{Details: d, Cause: err, Effects: effects}
}
func confirmed(resource tasks.ResourceRef, action string) tasks.Effect {
	return tasks.Effect{Resource: resource, Action: action, At: time.Now().UTC()}
}

// Translate backend categories at the repository boundary, preserving the cause
// for diagnostics without exposing backend details in HTTP responses.
func repositoryError(err error, message string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	var existing *tasks.DiagnosticError
	if !errors.As(err, &existing) {
		var detail vir.Error
		var pointer *vir.Error
		if errors.As(err, &pointer) {
			detail = *pointer
		} else if !errors.As(err, &detail) {
			return classifyError(err, message, args...)
		}
		err = &tasks.DiagnosticError{Details: tasks.Diagnostic{Backend: "libvirt", Message: detail.Message, Code: strconv.Itoa(int(detail.Code)), Domain: strconv.Itoa(int(detail.Domain)), Operation: fmt.Sprintf(message, args...)}, Cause: err}
	}
	return classifyError(err, message, args...)
}

func classifyError(err error, message string, args ...interface{}) error {
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
