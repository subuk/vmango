package api

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"subuk/vmango/ui"
)

var fingerprintedAsset = regexp.MustCompile(`^/assets/[^/]+-[A-Za-z0-9_-]{8,}\.[a-z0-9]+$`)

func (e *Environ) serveIndex(w http.ResponseWriter, r *http.Request) {
	b, err := fs.ReadFile(ui.Files(), "index.html")
	if err != nil {
		http.Error(w, "Frontend build is missing; run make ui.", 503)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	if r.Method != "HEAD" {
		w.Write(b)
	}
}

func frontendRoute(p string) bool {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	switch parts[0] {
	case "", "login", "nodes", "machines", "volumes", "networks", "keys", "tokens", "console":
		return true
	}
	return false
}

func (e *Environ) registerUI() {
	files := http.FileServer(http.FS(ui.Files()))
	e.router.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			writeAPIProblem(w, 405, "Method not allowed.")
			return
		}
		p := path.Clean(r.URL.Path)
		if p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasSuffix(p, "/ws") {
			writeAPIProblem(w, 404, "Route not found.")
			return
		}
		if p != "/" {
			if info, err := fs.Stat(ui.Files(), strings.TrimPrefix(p, "/")); err == nil && !info.IsDir() {
				w.Header().Set("X-Content-Type-Options", "nosniff")
				if p == "/vendor/novnc.html" {
					w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; object-src 'none'; base-uri 'none'")
				}
				if fingerprintedAsset.MatchString(p) {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/vendor/") || path.Ext(p) != "" || !frontendRoute(p) {
			http.NotFound(w, r)
			return
		}
		e.serveIndex(w, r)
	}))
}
