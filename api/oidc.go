package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"subuk/vmango/auth"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func localReturnPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") {
		return "/"
	}
	return u.RequestURI()
}

func (e *Environ) oidcCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: r.TLS != nil || e.cfg.SessionSecure || strings.HasPrefix(strings.ToLower(e.cfg.BaseUrl), "https://"), SameSite: http.SameSiteLaxMode, MaxAge: maxAge, Expires: time.Now().Add(time.Duration(maxAge) * time.Second)})
}

func (e *Environ) oidcError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/login/?auth_error="+url.QueryEscape(code), http.StatusSeeOther)
		return
	}
	writeAuthProblem(w, status, code, message)
}

func (e *Environ) OidcLoginRedirect(w http.ResponseWriter, r *http.Request) {
	if e.oauth2 == nil {
		writeAPIProblem(w, 404, "OpenID Connect is not configured.")
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		writeAuthProblem(w, 503, "auth_unavailable", "Unable to start login.")
		return
	}
	state := base64.RawURLEncoding.EncodeToString(b)
	verifier := oauth2.GenerateVerifier()
	e.oidcCookie(w, r, "oauthstate", state, 900)
	e.oidcCookie(w, r, "oauthverifier", verifier, 900)
	// The destination is validated again after the callback, before redirecting.
	e.oidcCookie(w, r, "oauthnext", base64.RawURLEncoding.EncodeToString([]byte(localReturnPath(r.URL.Query().Get("next")))), 900)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, e.oauth2.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (e *Environ) OidcLoginCallback(w http.ResponseWriter, r *http.Request) {
	if !e.apiSecure(r) {
		e.oidcError(w, r, 403, "transport_rejected", "HTTPS is required.")
		return
	}
	if e.oauth2 == nil || e.oidcp == nil {
		writeAPIProblem(w, 404, "OpenID Connect is not configured.")
		return
	}
	state, err := r.Cookie("oauthstate")
	if err != nil || state.Value == "" || subtle.ConstantTimeCompare([]byte(state.Value), []byte(r.URL.Query().Get("state"))) != 1 {
		e.oidcError(w, r, 400, "invalid_oidc_state", "Invalid login state.")
		return
	}
	verifier, vErr := r.Cookie("oauthverifier")
	next, _ := r.Cookie("oauthnext")
	for _, name := range []string{"oauthstate", "oauthverifier", "oauthnext"} {
		e.oidcCookie(w, r, name, "", -1)
	}
	if vErr != nil || verifier.Value == "" || r.URL.Query().Get("error") != "" {
		e.oidcError(w, r, 400, "oidc_login_failed", "Login was rejected or its transaction expired.")
		return
	}
	token, err := e.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier.Value))
	if err != nil {
		e.logger.Warn().Msg("OIDC code exchange failed")
		e.oidcError(w, r, 503, "oidc_unavailable", "Identity provider unavailable; try logging in again.")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		e.oidcError(w, r, 401, "invalid_oidc_token", "Invalid identity token.")
		return
	}
	idToken, err := e.oidcp.VerifierContext(r.Context(), &oidc.Config{ClientID: e.oauth2.ClientID}).Verify(r.Context(), raw)
	if err != nil {
		e.oidcError(w, r, 401, "invalid_oidc_token", "Invalid identity token.")
		return
	}
	var info struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err = idToken.Claims(&info); err != nil {
		e.oidcError(w, r, 401, "invalid_oidc_token", "Invalid identity claims.")
		return
	}
	p := auth.Principal{Method: "oidc", Issuer: idToken.Issuer, Subject: idToken.Subject, Email: info.Email, Name: info.Name, EmailVerified: info.Verified}
	_, refresh, expires, err := e.auth.Login(p)
	if err != nil {
		problem := authError(err).(*authProblem)
		e.oidcError(w, r, problem.Status, problem.Code, problem.Detail)
		return
	}
	cookie := e.cookie(refresh, expires)
	cookie.Secure = cookie.Secure || r.TLS != nil
	http.SetCookie(w, &cookie)
	destination := "/"
	if next != nil {
		if b, err := base64.RawURLEncoding.DecodeString(next.Value); err == nil {
			destination = localReturnPath(string(b))
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, destination, http.StatusFound)
}
