package util

import (
	"fmt"
)

type Error struct {
	Original error
	Message  string
}

func NewError(err error, msg string, args ...interface{}) Error {
	return Error{
		Original: err,
		Message:  fmt.Sprintf(msg, args...),
	}
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Message, e.Original)
}
