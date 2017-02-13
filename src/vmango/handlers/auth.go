package handlers

import (
	"fmt"
	"github.com/gorilla/csrf"
	"github.com/gorilla/schema"
	"net/http"
	"vmango/models"
	"vmango/web"
)

type loginFormData struct {
	Username string
	Password string
	CSRF     string
}

func Login(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	if req.Method != "POST" {
		ctx.Render.HTML(w, http.StatusOK, "login", map[string]interface{}{
			"Request": req,
			"Title":   "Login",
		})
		return nil
	}
	if err := req.ParseForm(); err != nil {
		return err
	}
	form := &loginFormData{}
	if err := schema.NewDecoder().Decode(form, req.PostForm); err != nil {
		return web.BadRequest(err.Error())
	}
	user := &models.User{Name: form.Username}
	if exists, err := ctx.AuthDB.Get(user); err != nil {
		return fmt.Errorf("failed to fetch user from auth database: %s", err)
	} else if !exists {
		ctx.Logger.WithField("username", form.Username).WithField("reason", "no user found").Warn("authentication failed")
		return web.BadRequest("authentication failed")
	}
	if valid := user.CheckPassword(form.Password); !valid {
		ctx.Logger.WithField("username", form.Username).WithField("reason", "invalid password").Warn("authentication failed")
		return web.BadRequest("authentication failed")
	}
	session := ctx.Session(req)
	session.SetAuthUser(form.Username)
	if err := session.Save(req, w); err != nil {
		return err
	}
	url, err := ctx.Router.Get("index").URL()
	if err != nil {
		panic(err)
	}
	redirectUrl := url.Path
	if next := req.URL.Query().Get("next"); next != "" {
		redirectUrl = next
	}
	http.Redirect(w, req, redirectUrl, http.StatusFound)
	return nil
}

func Logout(ctx *web.Context, w http.ResponseWriter, req *http.Request) error {
	session := ctx.Session(req)
	session.SetAuthUser("")
	if err := session.Save(req, w); err != nil {
		return err
	}
	http.Redirect(w, req, "/login/", http.StatusFound)
	return nil
}

func CSRFFailed(ctx *web.Context, w http.ResponseWriter, r *http.Request) error {
	errorText := fmt.Sprintf("%s - %s",
		http.StatusText(http.StatusForbidden),
		csrf.FailureReason(r),
	)
	return web.Forbidden(errorText)
}
