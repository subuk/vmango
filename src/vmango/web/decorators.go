package web

import (
	"context"
	"net/http"
	"strings"
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
		return next(ctx, w, req)
	}
}

func APIAuthenticationRequired(next HandlerFunc) HandlerFunc {
	return func(ctx *Context, w http.ResponseWriter, req *http.Request) error {
		if !ctx.CheckAPIAuth(req) {
			ctx.Render.JSON(w, http.StatusUnauthorized, map[string]string{
				"Error": "Authentication failed",
			})
			return nil
		}
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
