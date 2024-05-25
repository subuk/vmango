package web

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

func (env *Environ) PasswordLoginFormShow(rw http.ResponseWriter, req *http.Request) {
	env.render.HTML(rw, http.StatusOK, "login", map[string]interface{}{
		"Request": req,
		"Title":   "Login",
	})
}

func (env *Environ) OidcLoginRedirect(rw http.ResponseWriter, req *http.Request) {
	if env.oidcp == nil || env.oauth2 == nil {
		env.logger.Warn().Msg("please configure oidc before use")
		http.Error(rw, "OpenID not configured", http.StatusBadRequest)
		return
	}
	b := make([]byte, 16)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)
	cookie := http.Cookie{Name: "oauthstate", Value: state, Expires: time.Now().Add(15 * time.Minute), Path: "/"}
	http.SetCookie(rw, &cookie)
	u := env.oauth2.AuthCodeURL(state)
	http.Redirect(rw, req, u, http.StatusFound)
}

func (env *Environ) OidcLoginCallback(rw http.ResponseWriter, req *http.Request) {
	oauthState, err := req.Cookie("oauthstate")
	if err != nil {
		env.logger.Warn().Err(err).Msg("failed to read state cookie")
		http.Error(rw, "Invalid oauth state cookie", http.StatusBadRequest)
		return
	}

	if req.FormValue("state") != oauthState.Value {
		http.Error(rw, "Invalid oauth state cookie", http.StatusBadRequest)
		return
	}
	code := req.FormValue("code")
	token, err := env.oauth2.Exchange(req.Context(), code)
	if err != nil {
		http.Error(rw, "Failed to exchange code: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rawIdToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(rw, "No valid id_token was provided in response", http.StatusInternalServerError)
		return
	}
	verifier := env.oidcp.VerifierContext(req.Context(), &oidc.Config{ClientID: env.oauth2.ClientID})
	idToken, err := verifier.Verify(req.Context(), rawIdToken)
	if err != nil {
		http.Error(rw, "Failed to verify id_token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	claims := struct {
		Name  string
		Email string
	}{}

	if err := idToken.Claims(&claims); err != nil {
		http.Error(rw, "Failed to extract claims: "+err.Error(), http.StatusInternalServerError)
		return
	}

	user := &User{
		Id:            claims.Email,
		Email:         claims.Email,
		FullName:      claims.Name,
		Authenticated: true,
	}

	session := env.Session(req)
	session.SetAuthUser(user)
	if err := session.Save(req, rw); err != nil {
		http.Error(rw, "Session save failed:"+err.Error(), http.StatusInternalServerError)
		return
	}
	redirectUrl := req.URL.Query().Get("next")
	if redirectUrl == "" {
		redirectUrl = env.url("node-list").Path
	}
	http.Redirect(rw, req, redirectUrl, http.StatusFound)
}

func (env *Environ) PasswordLoginFormProcess(rw http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	email := req.Form.Get("Username")
	password := req.Form.Get("Password")
	user := env.checkPassword(email, password)
	if user == nil {
		env.render.HTML(rw, http.StatusUnauthorized, "login", map[string]interface{}{
			"Request": req,
			"Title":   "Login",
			"Error":   "Invalid username or password",
		})
		return
	}

	session := env.Session(req)
	session.SetAuthUser(user)
	if err := session.Save(req, rw); err != nil {
		http.Error(rw, "Session save failed:"+err.Error(), http.StatusInternalServerError)
		return
	}
	redirectUrl := req.URL.Query().Get("next")
	if redirectUrl == "" {
		redirectUrl = env.url("node-list").Path
	}
	http.Redirect(rw, req, redirectUrl, http.StatusFound)
}

func (env *Environ) Logout(rw http.ResponseWriter, req *http.Request) {
	session := env.Session(req)
	user := session.AuthUser()
	if user.Authenticated {
		session.Options.MaxAge = -1
		if err := session.Save(req, rw); err != nil {
			env.error(rw, req, err, "failed to save session", http.StatusInternalServerError)
			return
		}
		env.logger.Info().Str("user", user.Id).Msg("user logged out")
	}
	http.Redirect(rw, req, "/", http.StatusFound)
}
