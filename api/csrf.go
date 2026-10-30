package api

import (
	"net/http"
	"strings"
	"subuk/vmango/config"
)

func protectCrossOrigin(next http.Handler, cfg *config.APIConfig) http.Handler {
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := protection.Check(r); err != nil {
			writeAuthProblem(w, 403, "origin_rejected", "Cross-origin requests are disabled.")
			return
		}
		if (r.TLS != nil || cfg.SessionSecure || strings.HasPrefix(strings.ToLower(cfg.BaseUrl), "https://")) && r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if origin := r.Header.Get("Origin"); origin != "" && !strings.HasPrefix(origin, "https://") {
				writeAuthProblem(w, 403, "origin_rejected", "HTTPS origin required.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
