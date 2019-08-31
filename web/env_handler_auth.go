package web

import (
	"net/http"
)

func (env *Environ) PasswordLoginFormShow(rw http.ResponseWriter, req *http.Request) {
	env.render.HTML(rw, http.StatusOK, "login", map[string]interface{}{
		"Request": req,
		"Title":   "Login",
	})
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
		redirectUrl = env.url("index").Path
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
		env.logger.Info().Str("user", user.Email).Msg("user logged out")
	}
	http.Redirect(rw, req, "/", http.StatusFound)
}
