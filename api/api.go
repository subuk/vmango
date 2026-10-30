package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"subuk/vmango/auth"
	"subuk/vmango/compute"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humamux"
	"github.com/gorilla/mux"
)

type apiUserContext struct{}

type apiProvisioning struct {
	Stage            string   `json:"stage"`
	Node             string   `json:"node"`
	VM               string   `json:"vm"`
	VMCreated        bool     `json:"vm_created"`
	Volumes          []string `json:"volumes" nullable:"false"`
	OutcomeUncertain bool     `json:"outcome_uncertain" doc:"Reconcile resource state before retrying a mutation."`
}

type apiProblem struct {
	huma.ErrorModel
	Provisioning            *apiProvisioning `json:"provisioning,omitempty"`
	Code                    string           `json:"code,omitempty" doc:"Stable authentication/service error code when applicable."`
	RejectedBeforeExecution bool             `json:"rejected_before_execution,omitempty" doc:"Authentication guard rejected the request before any domain handler ran."`
}

func apiError(err error) error {
	if err == nil {
		return nil
	}
	status, detail := 500, "Operation failed; inspect resource state before retrying."
	switch {
	case errors.Is(err, compute.ErrConflict), errors.Is(err, compute.ErrKeyAlreadyExists):
		status, detail = 409, "Resource already exists, is busy, protected, or in a conflicting state."
	case errors.Is(err, compute.ErrNotFound), errors.Is(err, compute.ErrUnknownNode), errors.Is(err, compute.ErrVolumeNotFound), errors.Is(err, compute.ErrKeyNotFound), errors.Is(err, compute.ErrInterfaceNotFound):
		status, detail = 404, "Resource not found."
	case errors.Is(err, compute.ErrUnavailable):
		status, detail = 503, "Backend unavailable."
	case errors.Is(err, compute.ErrInvalid), errors.Is(err, compute.ErrInvalidImageRequest):
		status, detail = 422, "Request violates resource validation or backend capability constraints."
	}
	var partial *compute.ProvisioningError
	if errors.As(err, &partial) {
		return &apiProblem{ErrorModel: huma.ErrorModel{Type: "about:blank", Title: "Provisioning failed", Status: 500, Detail: "Partial provisioning; reconcile resources before retrying. No automatic rollback was performed."}, Provisioning: &apiProvisioning{partial.Stage, partial.Node, partial.VM, partial.VMCreated, volumeIDs(partial.Volumes), true}}
	}
	var validation *compute.ValidationError
	if errors.As(err, &validation) {
		return &huma.ErrorModel{Type: "about:blank", Title: "Validation failed", Status: 422, Detail: "Correct the indicated field.", Errors: []*huma.ErrorDetail{{Location: "body." + validation.Field, Message: validation.Message}}}
	}
	return &huma.ErrorModel{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail}
}

func writeAPIProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(&huma.ErrorModel{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail})
}

func (e *Environ) apiSecure(r *http.Request) bool {
	if r.TLS != nil || e.cfg.SessionSecure || strings.HasPrefix(strings.ToLower(e.cfg.BaseUrl), "https://") {
		return true
	}
	host := r.URL.Hostname()
	if host == "" {
		u := url.URL{Host: r.Host}
		host = u.Hostname()
	}
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	return net.ParseIP(peer).IsLoopback() && (net.ParseIP(host).IsLoopback() || host == "localhost")
}

// Check GET and WebSocket requests as well as mutation origins.
func (e *Environ) apiOriginAllowed(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
		secure := r.TLS != nil || strings.HasPrefix(strings.ToLower(e.cfg.BaseUrl), "https://") || e.cfg.SessionSecure
		if secure {
			return u.Scheme == "https"
		}
		return u.Scheme == "http"
	}
	return true
}

func (e *Environ) apiGuard(next http.Handler, protected bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !e.apiOriginAllowed(r) {
			writeAuthProblem(w, 403, "origin_rejected", "Cross-origin requests are disabled.")
			return
		}
		if !e.apiSecure(r) {
			writeAuthProblem(w, 403, "transport_rejected", "HTTPS is required except on loopback development connections.")
			return
		}
		if protected {
			raw := bearer(r)
			c, err := e.auth.Authenticate(raw)
			if err != nil {
				status, code, message := 503, "auth_unavailable", "Authentication storage unavailable; retry later."
				if errors.Is(err, auth.ErrInvalid) || errors.Is(err, auth.ErrNotFound) {
					status, code, message = 401, "invalid_credential", "A valid Bearer access token is required."
				}
				if errors.Is(err, auth.ErrExpired) {
					status, code, message = 401, "access_token_expired", "Access token expired."
				}
				writeAuthProblem(w, status, code, message)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), apiUserContext{}, c))
		}
		r = r.WithContext(context.WithValue(r.Context(), secureContext{}, r.TLS != nil))
		next.ServeHTTP(w, r)
	})
}

type secureContext struct{}

func bearer(r *http.Request) string {
	if len(r.Header.Values("Authorization")) != 1 {
		return ""
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func registerAPI[I, O any](api huma.API, method, path, id, summary string, handler func(context.Context, *I) (*O, error)) {
	status := 0
	if strings.HasPrefix(id, "create-") || id == "add-key" || id == "clone-volume" {
		status = 201
	}
	huma.Register(api, huma.Operation{DefaultStatus: status, Method: method, Path: "/api/v1" + path, OperationID: id, Summary: summary, Tags: []string{strings.Split(strings.TrimPrefix(path, "/"), "/")[0]}, Errors: []int{400, 401, 403, 404, 409, 422, 500, 503}, MaxBodyBytes: 2 << 20}, handler)
}

type apiEmpty struct{}
type apiResult[T any] struct {
	Body T `nullable:"false"`
}
type apiNodeInput struct {
	Node string `path:"node" minLength:"1"`
}
type apiVMInput struct {
	Node string `path:"node" minLength:"1"`
	VM   string `path:"vm" minLength:"1"`
}

func (e *Environ) registerAPI() {
	if e.cfg.APIDisabled {
		return
	}
	router := mux.NewRouter()
	cfg := huma.DefaultConfig("Vmango API", "1.0.0")
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.Transformers = []huma.Transformer{func(ctx huma.Context, status string, value any) (any, error) {
		// Validation locations and messages are useful; echoing submitted values can
		// expose cloud-init secrets when clients retain error responses.
		if problem, ok := value.(*huma.ErrorModel); ok {
			for _, detail := range problem.Errors {
				detail.Value = nil
			}
		}
		return value, nil
	}}
	cfg.CreateHooks = nil
	cfg.RejectUnknownQueryParameters = true
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{"bearer": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"}}
	cfg.Security = []map[string][]string{{"bearer": {}}}
	cfg.Info.Description = "Synchronous VM automation. Sizes are bytes. Partial failures and disconnected requests may have side effects; reconcile state before retrying. poweroff is a hard power-off. See /api/console-protocol for binary WebSocket framing."
	api := humamux.New(router, cfg)
	e.registerAuth(api)
	e.registerGlobalLists(api)
	e.registerDiscovery(api)
	e.registerVMOperations(api)
	e.registerVolumeOperations(api)
	e.registerKeyOperations(api)
	e.registerConsoles(api, router)
	partialSchema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(apiProblem{}), true, "ProvisioningProblem")
	for _, p := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{p.Get, p.Head, p.Post, p.Put, p.Patch, p.Delete, p.Options} {
			if op == nil {
				continue
			}
			for status, response := range op.Responses {
				if status == "default" || strings.HasPrefix(status, "4") || strings.HasPrefix(status, "5") {
					response.Content = map[string]*huma.MediaType{"application/problem+json": {Schema: partialSchema}}
				}
			}
		}
	}
	router.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		e.serveIndex(w, r)
	}).Methods("GET", "HEAD")
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeAPIProblem(w, 404, "API route not found.") })
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeAPIProblem(w, 405, "Method not allowed.") })
	e.router.PathPrefix("/api/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public := r.URL.Path == "/api/docs" || r.URL.Path == "/api/v1/auth/config" || r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/refresh" || r.URL.Path == "/api/v1/auth/logout"
		e.apiGuard(router, !public).ServeHTTP(w, r)
	}))
}

func volumeIDs(paths []string) []string {
	out := []string{}
	for _, p := range paths {
		out = append(out, volumeID(p))
	}
	return out
}
