package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"subuk/vmango/auth"
	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/tasks"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"golang.org/x/oauth2"
)

var AppVersion string

type Environ struct {
	administration *compute.AdministrationService
	clouds         *compute.CloudService
	logger         zerolog.Logger
	router         *mux.Router
	networks       *compute.NetworkService
	keys           *compute.KeyService
	volpools       *compute.VolumePoolService
	nodes          *compute.NodeService
	volumes        *compute.VolumeService
	vms            *compute.VirtualMachineService
	images         *compute.ImageService
	vmanager       *compute.VirtualMachineManager
	cfg            *config.APIConfig
	auth           *auth.Service
	tasks          *tasks.Service
	oauth2         *oauth2.Config
	oidcp          *oidc.Provider
}

func New(cfg *config.Config, logger zerolog.Logger,
	networks *compute.NetworkService, keys *compute.KeyService,
	volpools *compute.VolumePoolService, nodes *compute.NodeService,
	volumes *compute.VolumeService, vms *compute.VirtualMachineService,
	vmanager *compute.VirtualMachineManager, images *compute.ImageService,
	authentication *auth.Service,
	taskService *tasks.Service,
	administration ...*compute.AdministrationService,
) (http.Handler, error) {
	if cfg.API.APIDisabled {
		return nil, fmt.Errorf("the frontend requires the API; remove api_disabled after reviewing the migration guide")
	}
	if authentication == nil {
		return nil, fmt.Errorf("authentication service is required")
	}
	if taskService == nil {
		return nil, fmt.Errorf("durable task service is required")
	}
	if err := auth.ValidateKey(cfg.API.ConsoleSigningKey); err != nil {
		return nil, fmt.Errorf("console_signing_key: %w", err)
	}
	if cfg.API.ConsoleSigningKey == cfg.API.JWTSigningKey {
		return nil, fmt.Errorf("console_signing_key must differ from jwt_signing_key")
	}
	env := &Environ{cfg: &cfg.API, logger: logger, router: mux.NewRouter(), networks: networks, keys: keys, volpools: volpools, nodes: nodes, volumes: volumes, vms: vms, vmanager: vmanager, images: images, auth: authentication, tasks: taskService}
	if vmanager != nil {
		env.clouds = vmanager.Clouds
	}
	if len(administration) > 0 {
		env.administration = administration[0]
	} else {
		snapshot, err := cfg.AdministrationSnapshot(AppVersion)
		if err != nil {
			return nil, fmt.Errorf("build accepted administration snapshot: %w", err)
		}
		env.administration = compute.NewAdministrationService(snapshot, nil)
	}
	if cfg.API.Oidc.ClientId != "" {
		provider, err := oidc.NewProvider(context.Background(), cfg.API.Oidc.IssuerUrl)
		if err != nil {
			return nil, fmt.Errorf("initialize OIDC provider: %w", err)
		}
		env.oidcp = provider
		env.oauth2 = &oauth2.Config{ClientID: cfg.API.Oidc.ClientId, ClientSecret: cfg.API.Oidc.ClientSecret, RedirectURL: strings.TrimRight(cfg.API.BaseUrl, "/") + "/login-oidc-callback/", Scopes: cfg.API.Oidc.Scopes, Endpoint: provider.Endpoint()}
	}
	env.registerAPI()
	env.router.Handle("/login-oidc/", env.apiGuard(http.HandlerFunc(env.OidcLoginRedirect), false)).Methods("GET")
	// A provider returns through a cross-site GET; state and PKCE authenticate it.
	env.router.HandleFunc("/login-oidc-callback/", env.OidcLoginCallback).Methods("GET")
	env.registerUI()
	return protectCrossOrigin(env, &cfg.API), nil
}

func (e *Environ) ServeHTTP(w http.ResponseWriter, r *http.Request) { e.router.ServeHTTP(w, r) }
