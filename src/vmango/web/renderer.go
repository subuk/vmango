package web

import (
	"fmt"
	"github.com/unrolled/render"
	"html/template"
	"strings"
	"time"
)

func NewRenderer(templatePath string, ctx *Context) *render.Render {

	return render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: true,
		Directory:     templatePath,
		Funcs: []template.FuncMap{
			template.FuncMap{
				"LimitString": func(limit int, s string) string {
					slen := len(s)
					s = s[:limit]
					if slen > limit {
						s += " ..."
					}
					return s
				},
				"HasPrefix": strings.HasPrefix,
				"HumanizeDate": func(date time.Time) string {
					return date.Format("Mon Jan 2 15:04:05 -0700 MST 2006")
				},
				"Capitalize": strings.Title,
				"Url": func(name string, params ...string) (string, error) {
					route := ctx.Router.Get(name)
					if route == nil {
						return "", fmt.Errorf("route named %s not found", name)
					}
					url, err := route.URL(params...)
					if err != nil {
						return "", err
					}
					return url.Path, nil
				},
			},
		},
	})

}
