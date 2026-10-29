package web

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
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
	Volumes          []string `json:"volumes"`
	OutcomeUncertain bool     `json:"outcome_uncertain" doc:"Reconcile resource state before retrying a mutation."`
}

type apiProblem struct {
	huma.ErrorModel
	Provisioning *apiProvisioning `json:"provisioning,omitempty"`
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
	return &huma.ErrorModel{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail}
}

func writeAPIProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(&huma.ErrorModel{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail})
}

func (e *Environ) apiSecure(r *http.Request) bool {
	if r.TLS != nil || strings.HasPrefix(strings.ToLower(e.cfg.BaseUrl), "https://") {
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

// Check GET and WebSocket requests as well: browsers can cache Basic credentials.
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

func (e *Environ) apiGuard(next http.Handler, basic bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !e.apiOriginAllowed(r) {
			writeAPIProblem(w, 403, "Cross-origin requests are disabled.")
			return
		}
		if !e.apiSecure(r) {
			writeAPIProblem(w, 403, "HTTPS is required except on loopback development connections.")
			return
		}
		if basic {
			name, password, ok := r.BasicAuth()
			var user *User
			if ok {
				user = e.checkPassword(name, password)
			}
			if user == nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="Vmango API", charset="UTF-8"`)
				writeAPIProblem(w, 401, "Valid local credentials are required.")
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), apiUserContext{}, user.Id))
		}
		next.ServeHTTP(w, r)
	})
}

func registerAPI[I, O any](api huma.API, method, path, id, summary string, handler func(context.Context, *I) (*O, error)) {
	status := 0
	if strings.HasPrefix(id, "create-") || id == "add-key" || id == "clone-volume" {
		status = 201
	}
	huma.Register(api, huma.Operation{DefaultStatus: status, Method: method, Path: "/api/v1" + path, OperationID: id, Summary: summary, Tags: []string{strings.Split(strings.TrimPrefix(path, "/"), "/")[0]}, Errors: []int{400, 401, 403, 404, 409, 422, 500, 503}, MaxBodyBytes: 2 << 20}, handler)
}

type apiEmpty struct{}
type apiResult[T any] struct{ Body T }
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
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{"basic": {Type: "http", Scheme: "basic"}}
	cfg.Security = []map[string][]string{{"basic": {}}}
	cfg.Info.Description = "Synchronous VM automation. Sizes are bytes. Partial failures and disconnected requests may have side effects; reconcile state before retrying. poweroff is a hard power-off. See /api/console-protocol for binary WebSocket framing."
	api := humamux.New(router, cfg)
	e.registerDiscovery(api)
	e.registerVMOperations(api)
	e.registerVolumeOperations(api)
	e.registerKeyOperations(api)
	e.registerConsoles(api, router)
	partialSchema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(apiProblem{}), true, "ProvisioningProblem")
	api.OpenAPI().Paths["/api/v1/nodes/{node}/vms"].Post.Responses["500"].Content = map[string]*huma.MediaType{"application/problem+json": {Schema: partialSchema}}
	router.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Vmango API</title><link rel="stylesheet" href="/static/swagger-ui-5.34.0/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="/static/swagger-ui-5.34.0/swagger-ui-bundle.js"></script><script>SwaggerUIBundle({url:'/api/openapi.json',dom_id:'#swagger-ui',validatorUrl:null,persistAuthorization:false,withCredentials:false});</script></body></html>`))
	}).Methods("GET")
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeAPIProblem(w, 404, "API route not found.") })
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeAPIProblem(w, 405, "Method not allowed.") })
	e.router.PathPrefix("/api/").Handler(e.apiGuard(router, true))
}

func volumeIDs(paths []string) []string {
	out := []string{}
	for _, p := range paths {
		out = append(out, volumeID(p))
	}
	return out
}
