package web

import (
	"fmt"
	"github.com/unrolled/render"
	"html/template"
	"net/http"
	"strings"
	"time"
)

var SUFFIXES = []string{"b", "K", "M", "G", "P"}

func NewRenderer(version string, ctx *Context) *render.Render {

	return render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: true,
		Asset: func(name string) ([]byte, error) {
			return Asset(name)
		},
		AssetNames: func() []string {
			return AssetNames()
		},
		IndentJSON: true,
		Funcs: []template.FuncMap{
			template.FuncMap{
				"HumanizeBytes": func(max int, number uint64) string {
					i := 0
					for {
						if number < 1024 {
							break
						}
						number = number / 1024
						i++
						if i >= max || i >= len(SUFFIXES) {
							break
						}
					}
					return fmt.Sprintf("%d%s", number, SUFFIXES[i])
				},
				"LimitString": func(limit int, s string) string {
					slen := len(s)
					if slen <= limit {
						return s
					}
					s = s[:limit]
					if slen > limit {
						s += "..."
					}
					return s
				},
				"IsAuthenticated": func(req *http.Request) bool {
					return ctx.Session(req).IsAuthenticated()
				},
				"HasPrefix": strings.HasPrefix,
				"HumanizeDate": func(date time.Time) string {
					return date.Format("Mon Jan 2 15:04:05 -0700 MST 2006")
				},
				"Capitalize": strings.Title,
				"Static": func(filename string) (string, error) {
					route := ctx.Router.Get("static")
					if route == nil {
						panic("no 'static' route defined")
					}
					url, err := route.URL("name", filename)
					if err != nil {
						return "", err
					}
					return url.Path + "?v=" + version, nil
				},
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
