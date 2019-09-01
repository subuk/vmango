package web

import (
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	neturl "net/url"
	"strings"
	libcompute "subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/util"
	"time"

	"github.com/gorilla/csrf"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/rs/zerolog"
	"github.com/unrolled/render"
	"golang.org/x/crypto/bcrypt"
)

var AppVersion string

type Environ struct {
	render   *render.Render
	logger   zerolog.Logger
	router   *mux.Router
	sessions sessions.Store
	random   *rand.Rand
	compute  *libcompute.Service
	cfg      *config.WebConfig
}

func TemplateFuncs(env *Environ) []template.FuncMap {
	return []template.FuncMap{
		template.FuncMap{
			"CSRFField": func(req *http.Request) template.HTML {
				return csrf.TemplateField(req)
			},
			"Version": func() string {
				return AppVersion
			},
			"HumanizeBytes": func(max int, number uint64) string {
				var suffixes = []string{"b", "K", "M", "G", "T"}
				i := 0
				for {
					if number < 10240 {
						break
					}
					number = number / 1024
					i++
					if i >= max || i >= len(suffixes) {
						break
					}
				}
				return fmt.Sprintf("%d%s", number, suffixes[i])
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
				return env.Session(req).IsAuthenticated()
			},
			"HasPrefix": strings.HasPrefix,
			"HumanizeDate": func(date time.Time) string {
				return date.Format("Mon Jan 2 15:04:05 -0700 MST 2006")
			},
			"Capitalize": strings.Title,
			"Title": func(s string) string {
				return strings.Title(s)
			},
			"Join": func(sep string, a []string) string {
				return strings.Join(a, sep)
			},
			"JoinUint": func(sep string, uints []uint) string {
				a := []string{}
				for _, i := range uints {
					a = append(a, fmt.Sprintf("%d", i))
				}
				return strings.Join(a, sep)
			},
			"Static": func(filename string) (string, error) {
				route := env.router.Get("static")
				if route == nil {
					return "", fmt.Errorf("no 'static' route defined")
				}
				url, err := route.URL("name", filename)
				if err != nil {
					return "", err
				}
				return url.Path + "?v=" + env.cfg.StaticVersion, nil
			},
			"Url": func(name string, params ...string) *neturl.URL {
				return env.url(name, params...)
			},
			"DateTimeLong": func(dt time.Time) string {
				return dt.Format(time.UnixDate)
			},
		},
	}
}

func New(cfg *config.Config, logger zerolog.Logger, compute *libcompute.Service) http.Handler {
	env := &Environ{cfg: &cfg.Web}
	router := mux.NewRouter()
	renderer := render.New(render.Options{
		Extensions:    []string{".html"},
		IsDevelopment: cfg.Web.Debug,
		Asset:         Asset,
		AssetNames:    AssetNames,
		IndentJSON:    true,
		IndentXML:     true,
		Funcs:         TemplateFuncs(env),
	})

	sessionStore := sessions.NewCookieStore([]byte(cfg.Web.SessionSecret))
	sessionStore.Options.MaxAge = cfg.Web.SessionMaxAge
	sessionStore.Options.HttpOnly = true
	sessionStore.Options.Secure = cfg.Web.SessionSecure
	sessionStore.Options.Domain = cfg.Web.SessionDomain

	csrfOptions := []csrf.Option{
		csrf.FieldName("csrf"),
		csrf.ErrorHandler(http.HandlerFunc(env.CsrfError)),
		csrf.Secure(false),
	}
	csrfProtect := csrf.Protect([]byte(cfg.Web.SessionSecret), csrfOptions...)

	env.random = rand.New(rand.NewSource(time.Now().UnixNano()))
	env.render = renderer
	env.logger = logger
	env.router = router
	env.compute = compute
	env.sessions = sessionStore

	router.HandleFunc("/static/{name:.*}", env.Static(cfg)).Name("static")

	router.HandleFunc("/login/", env.PasswordLoginFormProcess).Name("login").Methods("POST")
	router.HandleFunc("/login/", env.PasswordLoginFormShow).Name("login")
	router.HandleFunc("/logout/", env.Logout).Name("logout")

	router.HandleFunc("/volumes/", env.authenticated(env.VolumeList)).Name("volume-list")
	router.HandleFunc("/volumes/add/", env.authenticated(env.VolumeAddFormProcess)).Methods("POST").Name("volume-add-form")
	router.HandleFunc("/volumes/{path}/delete/", env.authenticated(env.VolumeDeleteFormProcess)).Methods("POST").Name("volume-delete-form")
	router.HandleFunc("/volumes/{path}/delete/", env.authenticated(env.VolumeDeleteFormShow)).Name("volume-delete-form")
	router.HandleFunc("/volumes/{path}/clone/", env.authenticated(env.VolumeCloneFormProcess)).Methods("POST").Name("volume-clone-form")
	router.HandleFunc("/volumes/{path}/clone/", env.authenticated(env.VolumeCloneFormShow)).Name("volume-clone-form")
	router.HandleFunc("/volumes/{path}/resize/", env.authenticated(env.VolumeResizeFormProcess)).Methods("POST").Name("volume-resize-form")
	router.HandleFunc("/volumes/{path}/resize/", env.authenticated(env.VolumeResizeFormShow)).Name("volume-resize-form")

	router.HandleFunc("/networks/", env.authenticated(env.NetworkList)).Name("network-list")

	router.HandleFunc("/keys/", env.authenticated(env.KeyList)).Name("key-list")
	router.HandleFunc("/keys/add/", env.authenticated(env.KeyAddFormProcess)).Methods("POST").Name("key-add")
	router.HandleFunc("/keys/{fingerprint}/show/", env.authenticated(env.KeyShow)).Name("key-show")
	router.HandleFunc("/keys/{fingerprint}/delete/", env.authenticated(env.KeyDeleteFormProcess)).Methods("POST").Name("key-delete-form")
	router.HandleFunc("/keys/{fingerprint}/delete/", env.authenticated(env.KeyDeleteFormShow)).Name("key-delete-form")

	router.HandleFunc("/machines/", env.authenticated(env.VirtualMachineList)).Name("virtual-machine-list")
	router.HandleFunc("/machines/add/", env.authenticated(env.VirtualMachineAddFormProcess)).Methods("POST").Name("virtual-machine-add")
	router.HandleFunc("/machines/add/", env.authenticated(env.VirtualMachineAddFormShow)).Name("virtual-machine-add")
	router.HandleFunc("/machines/{id}/", env.authenticated(env.VirtualMachineDetail)).Name("virtual-machine-detail")
	router.HandleFunc("/machines/{id}/attach-disk/", env.authenticated(env.VirtualMachineAttachDiskFormProcess)).Methods("POST").Name("virtual-machine-attach-disk")
	router.HandleFunc("/machines/{id}/detach-volume/", env.authenticated(env.VirtualMachineDetachVolumeFormProcess)).Methods("POST").Name("virtual-machine-detach-volume")
	router.HandleFunc("/machines/{id}/attach-interface/", env.authenticated(env.VirtualMachineAttachInterfaceFormProcess)).Methods("POST").Name("virtual-machine-attach-interface")
	router.HandleFunc("/machines/{id}/detach-interface/", env.authenticated(env.VirtualMachineDetachInterfaceFormProcess)).Methods("POST").Name("virtual-machine-detach-interface")
	router.HandleFunc("/machines/{id}/set-state/{action}/", env.authenticated(env.VirtualMachineStateSetFormProcess)).Name("virtual-machine-state-form").Methods("POST")
	router.HandleFunc("/machines/{id}/set-state/{action}/", env.authenticated(env.VirtualMachineStateSetFormShow)).Name("virtual-machine-state-form")
	router.HandleFunc("/machines/{id}/delete/", env.authenticated(env.VirtualMachineDeleteFormProcess)).Name("virtual-machine-delete").Methods("POST")
	router.HandleFunc("/machines/{id}/delete/", env.authenticated(env.VirtualMachineDeleteFormShow)).Name("virtual-machine-delete")

	router.HandleFunc("/", env.authenticated(env.HostInfo)).Name("index")

	return csrfProtect(env)
}

func (env *Environ) error(rw http.ResponseWriter, req *http.Request, err error, message string, status int) {
	if err != nil {
		env.logger.Warn().Int("Status", status).Err(err).Msg("request error occured")
	}
	switch status {
	default:
		http.Error(rw, "Error: "+message+": "+err.Error(), status)
	case http.StatusNotFound:
		data := struct {
			Message string
		}{message}
		if err := env.render.HTML(rw, http.StatusNotFound, "404", data); err != nil {
			http.Error(rw, "failed to render template", http.StatusInternalServerError)
			return
		}
	}
}

func (e *Environ) url(name string, params ...string) *neturl.URL {
	route := e.router.Get(name)
	if route == nil {
		panic(fmt.Errorf("route named %s not found", name))
	}
	for i := 0; i < len(params); i++ {
		params[i] = strings.Replace(params[i], "/", "%2F", -1)
	}
	url, err := route.URL(params...)
	if err != nil {
		panic(util.NewError(err, "resolving failed with params %s", params))
	}
	return url
}

func (e *Environ) vars(request *http.Request) map[string]string {
	return mux.Vars(request)
}

func (env *Environ) authenticated(handler http.HandlerFunc) http.HandlerFunc {
	loginUrl := env.url("login")
	return func(rw http.ResponseWriter, request *http.Request) {
		session := env.Session(request)
		if !session.IsAuthenticated() {
			session.Values["next"] = request.URL.String()
			session.Save(request, rw)
			http.Redirect(rw, request, loginUrl.Path, http.StatusFound)
			return
		}
		handler(rw, request)
	}
}

func (env *Environ) checkPassword(email string, password string) *User {
	for _, user := range env.cfg.Users {
		if user.Email != email {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.HashedPassword), []byte(password)); err != nil {
			env.logger.Warn().Err(err).Msg("authentication failure")
			return nil
		}
		return &User{
			Email:         user.Email,
			FullName:      user.FullName,
			Authenticated: true,
		}
	}
	env.logger.Warn().Str("email", email).Msg("user not found")
	return nil
}

func (env *Environ) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	env.router.ServeHTTP(w, request)
}
