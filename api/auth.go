package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"subuk/vmango/auth"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

const refreshCookie = "vmango_refresh"

type authProblem struct {
	huma.ErrorModel
	Code                    string `json:"code"`
	RejectedBeforeExecution bool   `json:"rejected_before_execution,omitempty"`
}

func writeAuthProblem(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	if status == 401 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="Vmango API", error="invalid_token"`)
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(authProblem{huma.ErrorModel{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: message}, code, true})
}

func authError(err error) error {
	status, code, message := 503, "auth_unavailable", "Authentication storage unavailable; retry later."
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		status, code, message = 422, "invalid_request", "Provide a descriptive token name and a lifetime between 1 and 365 days."
	case errors.Is(err, auth.ErrExpired):
		status, code, message = 401, "access_token_expired", "Access token expired."
	case errors.Is(err, auth.ErrInvalid), errors.Is(err, auth.ErrNotFound):
		status, code, message = 401, "invalid_credential", "Invalid or revoked credential."
	case errors.Is(err, auth.ErrForbidden):
		status, code, message = 403, "credential_forbidden", "This credential cannot perform that operation."
	case errors.Is(err, auth.ErrLimit):
		status, code, message = 409, "credential_limit", "Credential limit reached; revoke unused credentials or wait for expired records to be cleaned up."
	}
	return &authProblem{huma.ErrorModel{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: message}, code, false}
}

func (p *authProblem) GetHeaders() http.Header {
	h := http.Header{"Cache-Control": []string{"no-store"}}
	if p.Status == 401 {
		h.Set("WWW-Authenticate", `Bearer realm="Vmango API", error="invalid_token"`)
	}
	return h
}

func claims(ctx context.Context) *auth.Claims {
	c, _ := ctx.Value(apiUserContext{}).(*auth.Claims)
	return c
}

type apiAuthConfig struct {
	Password     bool   `json:"password"`
	OIDC         bool   `json:"oidc"`
	OIDCTitle    string `json:"oidc_title,omitempty"`
	OIDCLoginURL string `json:"oidc_login_url,omitempty"`
}
type apiLoginInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"256"`
		Password string `json:"password" minLength:"1" maxLength:"4096"`
	}
	Host string `header:"Host"`
}
type apiRefreshInput struct {
	Cookie http.Cookie `cookie:"vmango_refresh"`
}
type apiCredentials struct {
	SetCookie    http.Cookie `header:"Set-Cookie"`
	CacheControl string      `header:"Cache-Control"`
	Body         struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
}
type apiLogout struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}
type apiMe struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Email      string       `json:"email"`
	Version    string       `json:"version"`
	LinksTitle string       `json:"links_title"`
	Links      []apiNavLink `json:"links"`
}
type apiNavLink struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}
type apiTokenCreateInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"128"`
		Days int    `json:"days" minimum:"1" maximum:"365" default:"90"`
	}
}
type apiTokenIDInput struct {
	ID string `path:"id" pattern:"^[a-f0-9]{64}$"`
}
type apiCreatedToken struct {
	auth.TokenMetadata
	Token string `json:"token"`
}

func (e *Environ) cookie(value string, expires time.Time) http.Cookie {
	secure := e.cfg.SessionSecure || strings.HasPrefix(strings.ToLower(e.cfg.BaseUrl), "https://")
	maxAge := int(time.Until(expires).Seconds())
	if value == "" {
		maxAge = -1
	}
	return http.Cookie{Name: refreshCookie, Value: value, Path: "/api/v1/auth/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge}
}
func (e *Environ) credentials(ctx context.Context, access, refresh string, expires time.Time) *apiCredentials {
	out := &apiCredentials{SetCookie: e.cookie(refresh, expires), CacheControl: "no-store"}
	if secure, _ := ctx.Value(secureContext{}).(bool); secure {
		out.SetCookie.Secure = true
	}
	out.Body.AccessToken = access
	out.Body.TokenType = "Bearer"
	out.Body.ExpiresIn = int(auth.AccessLifetime.Seconds())
	return out
}

func registerPublic[I, O any](api huma.API, method, path, id string, handler func(context.Context, *I) (*O, error)) {
	huma.Register(api, huma.Operation{Method: method, Path: "/api/v1/auth/" + path, OperationID: id, Tags: []string{"auth"}, MaxBodyBytes: 16 << 10, Security: []map[string][]string{}}, handler)
}

func (e *Environ) registerAuth(api huma.API) {
	registerPublic(api, "GET", "config", "auth-config", func(ctx context.Context, in *apiEmpty) (*apiResult[apiAuthConfig], error) {
		return &apiResult[apiAuthConfig]{Body: apiAuthConfig{len(e.cfg.Users) > 0, e.oauth2 != nil, e.cfg.Oidc.Title, "/login-oidc/"}}, nil
	})
	registerPublic(api, "POST", "login", "auth-login", func(ctx context.Context, in *apiLoginInput) (*apiCredentials, error) {
		p := e.cfg.AuthPolicy().VerifyPassword(in.Body.Username, in.Body.Password)
		if p == nil {
			return nil, authError(auth.ErrInvalid)
		}
		a, r, x, err := e.auth.Login(*p)
		if err != nil {
			return nil, authError(err)
		}
		return e.credentials(ctx, a, r, x), nil
	})
	registerPublic(api, "POST", "refresh", "auth-refresh", func(ctx context.Context, in *apiRefreshInput) (*apiCredentials, error) {
		a, r, x, err := e.auth.Refresh(in.Cookie.Value)
		if err != nil {
			return nil, authError(err)
		}
		return e.credentials(ctx, a, r, x), nil
	})
	registerPublic(api, "POST", "logout", "auth-logout", func(ctx context.Context, in *apiRefreshInput) (*apiLogout, error) {
		if err := e.auth.Logout(in.Cookie.Value); err != nil {
			return nil, authError(err)
		}
		return &apiLogout{e.cookie("", time.Unix(1, 0))}, nil
	})
	registerAPI(api, "GET", "/me", "get-me", "Get current identity and installation metadata", func(ctx context.Context, in *apiEmpty) (*apiResult[apiMe], error) {
		c := claims(ctx)
		out := apiMe{ID: c.Subject, Name: c.Principal.Name, Email: c.Principal.Email, Version: AppVersion, LinksTitle: e.cfg.LinksTitle, Links: []apiNavLink{}}
		for _, l := range e.cfg.Links {
			out.Links = append(out.Links, apiNavLink{l.Title, l.Url, l.Active})
		}
		return &apiResult[apiMe]{Body: out}, nil
	})
	registerAPI(api, "GET", "/tokens", "list-tokens", "List personal API token metadata", func(ctx context.Context, in *apiEmpty) (*apiResult[[]auth.TokenMetadata], error) {
		items, err := e.auth.ListTokens(claims(ctx))
		if err != nil {
			return nil, authError(err)
		}
		return &apiResult[[]auth.TokenMetadata]{Body: items}, nil
	})
	registerAPI(api, "POST", "/tokens", "create-token", "Create a personal JWT; returned once", func(ctx context.Context, in *apiTokenCreateInput) (*apiResult[apiCreatedToken], error) {
		m, raw, err := e.auth.CreateToken(claims(ctx), in.Body.Name, in.Body.Days)
		if err != nil {
			return nil, authError(err)
		}
		return &apiResult[apiCreatedToken]{Body: apiCreatedToken{m, raw}}, nil
	})
	registerAPI(api, "DELETE", "/tokens/{id}", "revoke-token", "Revoke a personal token and its active consoles", func(ctx context.Context, in *apiTokenIDInput) (*apiEmpty, error) {
		if err := e.auth.RevokeToken(claims(ctx), in.ID); err != nil {
			return nil, authError(err)
		}
		return nil, nil
	})
}
