package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"vmango/models"

	"github.com/Sirupsen/logrus"
)

const (
	FORMAT_HTML = iota
	FORMAT_JSON_API
)

type Decorator func(HandlerFunc) HandlerFunc

func SessionAuthenticationRequired(next HandlerFunc) HandlerFunc {
	return func(ctx *Context, w http.ResponseWriter, req *http.Request) error {
		session := ctx.Session(req)
		if !session.IsAuthenticated() {
			http.Redirect(w, req, "/login/?next="+req.URL.String(), http.StatusFound)
			return nil
		}
		username := session.AuthUser()
		user := &models.User{Name: username}
		if exists, err := ctx.AuthDB.Get(user); err != nil {
			return err
		} else if !exists {
			return BadRequest(fmt.Sprintf("authenticated user '%s' doesn't exist", username))
		}
		ctx.AuthUser = user
		return next(ctx, w, req)
	}
}

func APIAuthenticationRequired(next HandlerFunc) HandlerFunc {
	return func(ctx *Context, w http.ResponseWriter, req *http.Request) error {
		username := req.Header.Get("X-Vmango-User")
		if username == "" {
			ctx.Render.JSON(w, http.StatusUnauthorized, map[string]string{
				"Error": "Authentication failed",
			})
			return nil
		}
		password := req.Header.Get("X-Vmango-Pass")
		if password == "" {
			ctx.Render.JSON(w, http.StatusUnauthorized, map[string]string{
				"Error": "Authentication failed",
			})
			return nil
		}
		user := &models.User{Name: username}
		if exist, err := ctx.AuthDB.Get(user); err != nil {
			logrus.WithError(err).Warning("failed to fetch user")
			ctx.Render.JSON(w, http.StatusUnauthorized, map[string]string{
				"Error": "Authentication failed",
			})
			return nil
		} else if !exist {
			logrus.WithField("username", username).Warning("Basic auth failed: no user found")
			ctx.Render.JSON(w, http.StatusUnauthorized, map[string]string{
				"Error": "Authentication failed",
			})
			return nil
		}
		if !user.CheckPassword(password) {
			ctx.Render.JSON(w, http.StatusUnauthorized, map[string]string{
				"Error": "Authentication failed",
			})
			return nil
		}
		ctx.AuthUser = user
		return next(ctx, w, req)
	}
}

func ForceJsonResponse(next HandlerFunc) HandlerFunc {
	return func(ctx *Context, w http.ResponseWriter, req *http.Request) error {
		*req = *req.WithContext(context.WithValue(req.Context(), "format", FORMAT_JSON_API))
		return next(ctx, w, req)
	}
}

func LimitMethods(methods ...string) Decorator {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context, w http.ResponseWriter, req *http.Request) error {
			for _, method := range methods {
				if strings.ToUpper(req.Method) == strings.ToUpper(method) {
					return next(ctx, w, req)
				}
			}
			return NotImplemented()
		}
	}
}

func ApplyDecorators(handler HandlerFunc, decorators ...Decorator) HandlerFunc {
	for _, dec := range decorators {
		handler = dec(handler)
	}
	return handler
}
