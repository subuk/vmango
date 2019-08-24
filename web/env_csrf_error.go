package web

import (
	"fmt"
	"net/http"

	"github.com/gorilla/csrf"
)

func (env *Environ) CsrfError(rw http.ResponseWriter, req *http.Request) {
	errorText := fmt.Sprintf("%s - %s",
		http.StatusText(http.StatusForbidden),
		csrf.FailureReason(req),
	)
	http.Error(rw, errorText, http.StatusBadRequest)
}
