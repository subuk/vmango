package web

import (
	"encoding/gob"
)

func init() {
	gob.Register(&User{})
}
