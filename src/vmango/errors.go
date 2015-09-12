package vmango

type HandlerError struct {
	Message string
}

func (e *HandlerError) Error() string {
	return e.Message
}

type ErrNotFound struct {
	*HandlerError
}

func NotFound(msg string) *ErrNotFound {
	return &ErrNotFound{&HandlerError{msg}}
}

type ErrForbidden struct {
	*HandlerError
}

func Forbidden(msg string) *ErrForbidden {
	return &ErrForbidden{&HandlerError{msg}}
}

type ErrBadRequest struct {
	*HandlerError
}

func BadRequest(msg string) *ErrBadRequest {
	return &ErrBadRequest{&HandlerError{msg}}
}

type ErrRedirect struct {
	*HandlerError
}

func Redirect(url string) *ErrRedirect {
	return &ErrRedirect{&HandlerError{url}}
}
