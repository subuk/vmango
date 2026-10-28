package web

import (
	"net/http"
	"net/url"
	"strings"

	"subuk/vmango/config"
)

func protectCrossOrigin(next http.Handler, cfg *config.WebConfig) http.Handler {
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := protection.Check(r); err != nil {
			http.Error(w, "Forbidden - "+err.Error(), http.StatusForbidden)
			return
		}
		// Preserve HTTP-to-HTTPS downgrade protection for older browsers that
		// send Origin without Sec-Fetch-Site, including behind TLS proxies.
		secure := r.TLS != nil || cfg.SessionSecure || strings.HasPrefix(strings.ToLower(cfg.BaseUrl), "https://")
		if secure && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Scheme != "https" {
					http.Error(w, "Forbidden - insecure origin", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
