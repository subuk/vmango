// Package installer owns the temporary setup session, independent of the web server.
package installer

import (
	"context"
	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/installer/richt"
)

type Host struct {
	ID        string `json:"id"`
	Cloud     string `json:"cloud"`
	Zone      string `json:"zone"`
	Address   string `json:"address"`
	User      string `json:"user"`
	Port      int    `json:"port"`
	Local     bool   `json:"local"`
	Sudo      bool   `json:"sudo"`
	Network   string `json:"network"`
	Subnet    string `json:"subnet"`
	DHCPStart string `json:"dhcpStart"`
	DHCPEnd   string `json:"dhcpEnd"`
	Pool      string `json:"pool"`
	PoolPath  string `json:"poolPath"`
	Selected  bool   `json:"selected"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
}

type APISettings struct {
	Listen      string `json:"listen"`
	BaseURL     string `json:"baseURL"`
	TLSCertFile string `json:"tlsCertFile"`
	TLSKeyFile  string `json:"tlsKeyFile"`
	AdminUser   string `json:"adminUser"`
	Password    string `json:"password,omitempty"`
}
type ControllerInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`
	User     string `json:"user"`
}
type CatalogEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Cloud struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Zones []CatalogEntry `json:"zones"`
}
type Review struct {
	ID      string   `json:"id"`
	Stage   string   `json:"stage"`
	Summary []string `json:"summary"`
	Output  string   `json:"output,omitempty"`
}
type Result struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Message string `json:"message"`
}
type State struct {
	Revision      uint64         `json:"revision"`
	ConfigPath    string         `json:"configPath"`
	Exists        bool           `json:"exists"`
	Controller    ControllerInfo `json:"controller"`
	Phase         string         `json:"phase"`
	Busy          bool           `json:"busy"`
	Error         string         `json:"error,omitempty"`
	Hosts         []Host         `json:"hosts"`
	API           APISettings    `json:"api"`
	Clouds        []Cloud        `json:"clouds"`
	Plan          *Review        `json:"plan,omitempty"`
	Results       []Result       `json:"results"`
	Logs          []string       `json:"logs"`
	BackupPath    string         `json:"backupPath,omitempty"`
	NeedsAdmin    bool           `json:"needsAdmin"`
	CanResume     bool           `json:"canResume"`
	AllowedStages []string       `json:"allowedStages"`
}
type Answers struct {
	Revision uint64      `json:"revision"`
	Hosts    []Host      `json:"hosts"`
	API      APISettings `json:"api"`
}

type Provisioner interface {
	Inspect(context.Context, richt.Host, func(string)) (richt.Inspection, error)
	Prepare(context.Context, []richt.Target, func(string)) (*richt.Plan, error)
	Apply(context.Context, *richt.Plan, func(string)) error
	VerifyPlan(*richt.Plan) error
}
type Resources interface {
	Prepare(context.Context, string, compute.HostPreparationSpec) (*compute.HostPreparationPlan, error)
	Apply(context.Context, *compute.HostPreparationPlan) error
}
type Options struct {
	Document    *config.InstallDocument
	StateDir    string
	Provisioner Provisioner
	Resources   func([]Host) Resources
	Parallelism int
}
