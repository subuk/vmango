package api

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Validate cloud membership before reads, command submission and native
// WebSocket handshakes. A resource URL cannot address another cloud's host.
func (e *Environ) cloudScopeGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		if cloud, scoped := vars["cloud"]; scoped {
			if e.clouds == nil {
				writeAPIProblem(w, 404, "Cloud not found.")
				return
			}
			_, err := e.clouds.Get(cloud)
			if node := vars["node"]; err == nil && node != "" {
				_, _, err = e.clouds.ForHost(cloud, node)
			}
			if err != nil {
				writeAPIProblem(w, 404, "Cloud resource not found.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
