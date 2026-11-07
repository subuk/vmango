package installer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/installer/richt"
	"subuk/vmango/util"
	"sync"
	"time"
)

type saved struct {
	Version                    int
	ConfigPath, OriginalDigest string
	State                      State
	PasswordHash               string
	Inspections                map[string]richt.Inspection
	HostReady, ResourceReady   map[string]string
	RichtPlan                  *richt.Plan
	ResourcePlans              []*compute.HostPreparationPlan
	ReadyPlans                 map[string]*compute.HostPreparationPlan
	ConfigDigest               string
	ResourceDigest             string
	Approved                   bool
}

type Controller struct {
	mu        sync.Mutex
	opts      Options
	data      saved
	document  *config.InstallDocument
	candidate *config.InstallCandidate
	cancel    context.CancelFunc
	done      chan struct{}
	lock      *os.File
}

func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func bytesDigest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func randomID() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hostDigest(h Host) string { h.Selected = false; h.Status = ""; h.Message = ""; return hash(h) }

func New(opts Options) (*Controller, error) {
	if opts.Document == nil || opts.Provisioner == nil || opts.Resources == nil {
		return nil, fmt.Errorf("installer dependencies are required")
	}
	if opts.Parallelism == 0 {
		opts.Parallelism = 3
	}
	if opts.Parallelism < 1 || opts.Parallelism > 16 {
		return nil, fmt.Errorf("parallelism must be 1..16")
	}
	if opts.StateDir == "" {
		base, e := os.UserCacheDir()
		if e != nil {
			return nil, e
		}
		opts.StateDir = filepath.Join(base, "vmango", "install", bytesDigest([]byte(opts.Document.Path()))[:24])
	}
	if err := privateDirectory(opts.StateDir); err != nil {
		return nil, err
	}
	// The lock lives beside the fixed config, independent of a selected state directory.
	lock, err := lockConfig(opts.Document.Path() + ".install.lock")
	if err != nil {
		return nil, err
	}
	c := &Controller{opts: opts, document: opts.Document, lock: lock}
	c.data = saved{Version: 1, ConfigPath: opts.Document.Path(), OriginalDigest: bytesDigest(opts.Document.Bytes()), Inspections: map[string]richt.Inspection{}, HostReady: map[string]string{}, ResourceReady: map[string]string{}, ReadyPlans: map[string]*compute.HostPreparationPlan{}}
	s := &c.data.State
	s.ConfigPath = c.data.ConfigPath
	s.Exists = opts.Document.Exists()
	s.Phase = "settings"
	s.Revision = 1
	s.Hosts = []Host{}
	s.Clouds = []Cloud{}
	s.Results = []Result{}
	s.Logs = []string{}
	s.Controller.OS = runtime.GOOS
	s.Controller.Arch = runtime.GOARCH
	s.Controller.Hostname, _ = os.Hostname()
	if u, e := user.Current(); e == nil {
		s.Controller.User = u.Username
	}
	c.populate()
	if err := c.restore(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Controller) populate() {
	cfg := c.document.Config()
	s := &c.data.State
	s.API = APISettings{Listen: cfg.API.Listen, BaseURL: publicAPIURL(cfg.API.BaseUrl), TLSCertFile: cfg.API.TLSCertFile, TLSKeyFile: cfg.API.TLSKeyFile, AdminUser: "admin"}
	if !c.document.Exists() {
		s.API.Listen = "127.0.0.1:8080"
		s.API.BaseURL = "http://localhost:8080"
	}
	s.NeedsAdmin = len(cfg.API.Users) == 0 && cfg.API.Oidc.IssuerUrl == ""
	for _, cloud := range cfg.Clouds {
		entry := Cloud{ID: cloud.ID, Name: cloud.Name, Zones: []CatalogEntry{}}
		for _, z := range cloud.Zones {
			entry.Zones = append(entry.Zones, CatalogEntry{z.ID, z.Name})
		}
		s.Clouds = append(s.Clouds, entry)
		for _, h := range cloud.Hosts {
			x := Host{ID: h.Node, Cloud: cloud.ID, Zone: h.Zone, Network: "default", Pool: h.ImagePool, Subnet: "192.168.122.0/24", DHCPStart: "192.168.122.2", DHCPEnd: "192.168.122.254", PoolPath: "/var/lib/libvirt/images"}
			if x.Pool == "" {
				x.Pool = "default"
			}
			for _, n := range h.Networks {
				if n.Network != "" {
					x.Network = n.Network
					break
				}
			}
			for _, p := range h.Storage {
				x.Pool = p.Pool
				break
			}
			u, e := url.Parse(h.Uri)
			if e == nil {
				x.Local = u.Host == ""
				x.Address = u.Hostname()
				if u.User != nil {
					x.User = u.User.Username()
				}
				if u.Port() != "" {
					fmt.Sscan(u.Port(), &x.Port)
				}
			}
			if x.Local && runtime.GOOS != "linux" {
				x.Status = "unsupported"
				x.Message = "Local macOS hypervisor provisioning is outside installer scope."
			}
			s.Hosts = append(s.Hosts, x)
		}
	}
	if len(s.Clouds) == 0 {
		s.Clouds = []Cloud{{ID: "local", Name: "Local", Zones: []CatalogEntry{{ID: "local", Name: "Local"}}}}
	}
}

func (c *Controller) restore() error {
	p := filepath.Join(c.opts.StateDir, "session.json")
	info, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 8<<20 {
		return fmt.Errorf("unsafe or oversized installer recovery state")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var old saved
	if err = json.Unmarshal(b, &old); err != nil {
		return fmt.Errorf("invalid installer recovery state; preserve it and use a new --state-dir")
	}
	if old.Version != 1 || old.ConfigPath != c.data.ConfigPath {
		return fmt.Errorf("incompatible installer recovery state")
	}
	old.State.API.BaseURL = publicAPIURL(old.State.API.BaseURL)
	if old.State.Phase == "complete" {
		return nil
	}
	if old.OriginalDigest != c.data.OriginalDigest {
		if old.Approved && old.State.Plan != nil && old.State.Plan.Stage == "config" && old.ConfigDigest == c.data.OriginalDigest {
			// Publication may precede the last progress write. Never apply that stage twice.
			old.State.Busy = false
			old.State.Phase = "complete"
			old.State.Plan = nil
			old.State.CanResume = false
			old.State.Exists = true
			old.State.Error = "Configuration publication was recovered. Start or restart Vmango normally."
			old.State.NeedsAdmin = false
			old.State.Controller = c.data.State.Controller
			if old.State.Exists && old.OriginalDigest != bytesDigest(nil) {
				old.State.BackupPath = c.data.ConfigPath + ".backup-" + old.OriginalDigest
			}
			old.PasswordHash = ""
			old.Approved = false
			c.data = old
			return c.persistLocked()
		}
		return fmt.Errorf("configuration changed since interrupted installation; preserve recovery state and start a fresh run with --state-dir")
	}
	if old.State.Plan == nil {
		return nil
	}
	if old.Inspections == nil || old.HostReady == nil || old.ResourceReady == nil {
		return fmt.Errorf("incomplete installer recovery state")
	}
	if old.ReadyPlans == nil {
		old.ReadyPlans = map[string]*compute.HostPreparationPlan{}
	}
	old.State.Controller = c.data.State.Controller
	old.State.Busy = false
	old.State.CanResume = old.Approved
	old.State.Error = "Interrupted installation loaded. Review the saved stage before continuing."
	c.data = old
	if old.State.Plan.Stage == "config" {
		candidate, e := c.document.Prepare(c.edits())
		if e != nil {
			return e
		}
		if candidate.Digest != old.ConfigDigest {
			return fmt.Errorf("saved configuration candidate changed")
		}
		c.candidate = candidate
	}
	return nil
}

func privateDirectory(p string) error {
	if err := os.MkdirAll(p, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("installer state directory must be private (0700) and not a symlink")
	}
	return nil
}

func (c *Controller) persistLocked() error {
	b, err := json.Marshal(c.data)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(c.opts.StateDir, ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), filepath.Join(c.opts.StateDir, "session.json")); err != nil {
		return err
	}
	d, err := os.Open(c.opts.StateDir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (c *Controller) Close() error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	done := c.done
	c.mu.Unlock()
	if done != nil {
		<-done
	}
	if c.lock != nil {
		return c.lock.Close()
	}
	return nil
}

func (c *Controller) Snapshot() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.data.State
	s.API.BaseURL = publicAPIURL(s.API.BaseURL)
	s.AllowedStages = []string{}
	if !s.Busy && s.Phase != "complete" {
		s.AllowedStages = append(s.AllowedStages, "hosts")
		if c.ready(c.data.HostReady) {
			s.AllowedStages = append(s.AllowedStages, "resources")
		}
		if c.ready(c.data.ResourceReady) {
			s.AllowedStages = append(s.AllowedStages, "config")
		}
	}
	b, _ := json.Marshal(s)
	var result State
	json.Unmarshal(b, &result)
	return result
}
func (c *Controller) ready(m map[string]string) bool {
	n := 0
	for _, h := range c.data.State.Hosts {
		if h.Selected {
			n++
			if m[h.ID] != hostDigest(h) {
				return false
			}
		}
	}
	return n > 0
}
func (c *Controller) check(rev uint64) error {
	if c.data.State.Phase == "complete" {
		return fmt.Errorf("installation is complete; start vmango install again to reload current configuration and hosts")
	}
	if c.data.State.Busy {
		return fmt.Errorf("an installer operation is already running")
	}
	if rev != c.data.State.Revision {
		return fmt.Errorf("answers changed; reload the current session")
	}
	return nil
}

func (c *Controller) Answers(a Answers) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(a.Revision); err != nil {
		return err
	}
	if len(a.Hosts) > 32 {
		return fmt.Errorf("at most 32 hosts can be configured in a run")
	}
	seen := map[string]bool{}
	targets := map[string]bool{}
	for i := range a.Hosts {
		h := &a.Hosts[i]
		h.Status = ""
		h.Message = ""
		if h.ID == "" || seen[h.ID] || strings.ContainsAny(h.ID, "/\\\x00\n\r") {
			return fmt.Errorf("host IDs must be unique and nonempty")
		}
		seen[h.ID] = true
		if !h.Selected {
			continue
		}
		if h.Local && runtime.GOOS != "linux" {
			return fmt.Errorf("local hypervisor setup requires Linux")
		}
		if h.Port < 0 || h.Port > 65535 {
			return fmt.Errorf("invalid SSH port")
		}
		if h.Cloud == "" || h.Zone == "" {
			return fmt.Errorf("select a cloud and zone for each host")
		}
		if !h.Local && (h.Address == "" || strings.HasPrefix(h.Address, "-") || strings.ContainsAny(h.Address, " \t\r\n/@")) {
			return fmt.Errorf("use an SSH hostname or alias, with a separate user")
		}
		if strings.HasPrefix(h.User, "-") || strings.ContainsAny(h.User, " \t\r\n/@") {
			return fmt.Errorf("invalid SSH user")
		}
		target := fmt.Sprintf("%t:%s:%d", h.Local, h.Address, h.Port)
		if targets[target] {
			return fmt.Errorf("the same target must not be configured twice")
		}
		targets[target] = true
		if _, err := c.uri(*h); err != nil {
			return err
		}
		if err := compute.ValidateHostPreparationSpec(resourceSpec(*h)); err != nil {
			return fmt.Errorf("host %s: %w", h.ID, err)
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("select at least one host")
	}
	if err := c.validateAPISelection(a.API); err != nil {
		return err
	}
	passwordHash := c.data.PasswordHash
	if a.API.Password != "" {
		if len(a.API.Password) < 12 || len(a.API.Password) > 72 {
			return fmt.Errorf("administrator password must contain 12..72 bytes")
		}
		b, e := bcrypt.GenerateFromPassword([]byte(a.API.Password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		passwordHash = string(b)
	}
	a.API.Password = ""
	if c.data.State.NeedsAdmin && passwordHash == "" {
		return fmt.Errorf("provide an initial administrator password")
	}
	if passwordHash != "" && a.API.AdminUser == "" {
		return fmt.Errorf("administrator name is required")
	}
	old := c.data
	oldCandidate := c.candidate
	c.data.PasswordHash = passwordHash
	c.data.State.API = a.API
	c.data.State.Hosts = a.Hosts
	c.data.State.Revision++
	c.data.State.Plan = nil
	c.data.State.Phase = "settings"
	c.data.State.Error = ""
	c.data.State.CanResume = false
	c.data.Approved = false
	c.data.RichtPlan = nil
	c.data.ResourcePlans = nil
	c.data.ConfigDigest = ""
	c.candidate = nil
	if _, err := c.document.Prepare(c.edits()); err != nil {
		c.data = old
		c.candidate = oldCandidate
		return err
	}
	if err := c.persistLocked(); err != nil {
		c.data = old
		c.candidate = oldCandidate
		return err
	}
	return nil
}

func validateAPI(a APISettings) error {
	host, _, err := net.SplitHostPort(a.Listen)
	if err != nil {
		return fmt.Errorf("API listen address must include a host and port")
	}
	if (a.TLSCertFile == "") != (a.TLSKeyFile == "") {
		return fmt.Errorf("supply both TLS certificate and key")
	}
	u, err := url.Parse(a.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("provide an absolute HTTP or HTTPS API base URL")
	}
	loop := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		loop = ip.IsLoopback()
	}
	if !loop && u.Scheme != "https" {
		return fmt.Errorf("non-loopback API access requires an HTTPS proxy or native TLS")
	}
	if a.TLSCertFile != "" && u.Scheme != "https" {
		return fmt.Errorf("native TLS requires an HTTPS base URL")
	}
	if a.TLSCertFile != "" {
		if _, err := tls.LoadX509KeyPair(util.ExpandHomeDir(a.TLSCertFile), util.ExpandHomeDir(a.TLSKeyFile)); err != nil {
			return fmt.Errorf("cannot read a matching TLS certificate and key: %w", err)
		}
	}
	return nil
}

func (c *Controller) validateAPISelection(a APISettings) error {
	a.BaseURL = c.selectedAPIURL(a.BaseURL)
	old := c.document.Config().API
	if c.document.Exists() && a.Listen == old.Listen && a.BaseURL == old.BaseUrl && a.TLSCertFile == old.TLSCertFile && a.TLSKeyFile == old.TLSKeyFile {
		if a.TLSCertFile != "" {
			_, err := tls.LoadX509KeyPair(util.ExpandHomeDir(a.TLSCertFile), util.ExpandHomeDir(a.TLSKeyFile))
			return err
		}
		return nil
	}
	return validateAPI(a)
}

// publicAPIURL excludes private URL components from browser and recovery state.
// Invalid legacy values are not repeated in diagnostics or form defaults.
func publicAPIURL(value string) string {
	if value == "" {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "[existing URL hidden]"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

// A form round trip of the safe projection must not rewrite an existing URL.
// Changed URLs are validated before being used in configuration edits.
func (c *Controller) selectedAPIURL(value string) string {
	old := c.document.Config().API.BaseUrl
	if c.document.Exists() && value == publicAPIURL(old) {
		return old
	}
	return value
}

func (c *Controller) uri(h Host) (string, error) {
	return ConnectionURI(c.document, h)
}

// ConnectionURI preserves the exact connection of every existing host.
func ConnectionURI(doc *config.InstallDocument, h Host) (string, error) {
	for _, old := range doc.Config().Hosts() {
		if old.Node == h.ID {
			u, e := url.Parse(old.Uri)
			if e != nil {
				return "", e
			}
			port := 0
			fmt.Sscan(u.Port(), &port)
			username := ""
			if u.User != nil {
				username = u.User.Username()
			}
			if h.Local != (u.Host == "") || h.Address != u.Hostname() || h.User != username || h.Port != port {
				return "", fmt.Errorf("existing host %s connection cannot be changed by the installer", h.ID)
			}
			return old.Uri, nil
		}
	}
	if h.Local {
		return "qemu:///system", nil
	}
	u := url.URL{Scheme: "qemu+ssh", Host: h.Address, Path: "/system"}
	if h.Port != 0 {
		u.Host = net.JoinHostPort(h.Address, fmt.Sprint(h.Port))
	} else if strings.Contains(h.Address, ":") && !strings.HasPrefix(h.Address, "[") {
		u.Host = "[" + h.Address + "]"
	}
	if h.User != "" {
		u.User = url.User(h.User)
	}
	return u.String(), nil
}

func (c *Controller) edits() config.InstallEdits {
	a := c.data.State.API
	a.BaseURL = c.selectedAPIURL(a.BaseURL)
	e := config.InstallEdits{API: &config.InstallAPI{Listen: a.Listen, BaseURL: a.BaseURL, TLSCertFile: a.TLSCertFile, TLSKeyFile: a.TLSKeyFile, AdminUser: a.AdminUser, PasswordHash: c.data.PasswordHash}}
	old := c.document.Config().API
	if c.document.Exists() && c.data.PasswordHash == "" && a.Listen == old.Listen && a.BaseURL == old.BaseUrl && a.TLSCertFile == old.TLSCertFile && a.TLSKeyFile == old.TLSKeyFile {
		e.API = nil
	}
	for _, h := range c.data.State.Hosts {
		if h.Selected {
			uri, _ := c.uri(h)
			e.Hosts = append(e.Hosts, config.InstallHost{Cloud: h.Cloud, Zone: h.Zone, Node: h.ID, URI: uri, Network: h.Network, Subnet: h.Subnet, DHCPStart: h.DHCPStart, DHCPEnd: h.DHCPEnd, Pool: h.Pool, PoolPath: h.PoolPath})
		}
	}
	return e
}

func transport(h Host) richt.Host {
	return richt.Host{ID: h.ID, Address: h.Address, User: h.User, Port: h.Port, Local: h.Local, Sudo: h.Sudo}
}
func resourceSpec(h Host) compute.HostPreparationSpec {
	return compute.HostPreparationSpec{Network: h.Network, Subnet: h.Subnet, DHCPStart: h.DHCPStart, DHCPEnd: h.DHCPEnd, Pool: h.Pool, PoolPath: h.PoolPath}
}
func (c *Controller) selectedLocked() []Host {
	var hs []Host
	for _, h := range c.data.State.Hosts {
		if h.Selected {
			hs = append(hs, h)
		}
	}
	return hs
}

func (c *Controller) log(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	line = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\t' {
			return -1
		}
		return r
	}, line)
	if len(line) > 4096 {
		line = line[:4096] + "…"
	}
	c.data.State.Logs = append(c.data.State.Logs, line)
	if len(c.data.State.Logs) > 300 {
		c.data.State.Logs = c.data.State.Logs[len(c.data.State.Logs)-300:]
	}
}
func (c *Controller) result(id, state, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.data.State.Results {
		if c.data.State.Results[i].ID == id {
			c.data.State.Results[i] = Result{id, state, message}
			return
		}
	}
	c.data.State.Results = append(c.data.State.Results, Result{id, state, message})
}

func (c *Controller) startLocked(run func(context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	c.data.State.Busy = true
	c.data.State.Error = ""
	c.data.State.CanResume = false
	if err := c.persistLocked(); err != nil {
		cancel()
		close(done)
		c.data.State.Busy = false
		return err
	}
	go func() {
		defer close(done)
		err := run(ctx)
		cancel()
		c.mu.Lock()
		defer c.mu.Unlock()
		c.cancel = nil
		c.data.State.Busy = false
		if err != nil {
			c.data.State.Error = err.Error()
			c.data.State.CanResume = c.data.Approved && c.data.State.Plan != nil
		}
		if e := c.persistLocked(); e != nil {
			c.data.State.Error = "Cannot save installation progress: " + e.Error() + ". Re-observe targets before retrying."
		}
	}()
	return nil
}
func (c *Controller) Cancel() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel == nil {
		return fmt.Errorf("no operation is running")
	}
	c.cancel()
	c.data.State.Error = "Cancellation requested; waiting for executors. Remote actions may already have taken effect."
	return nil
}

func (c *Controller) Inspect(rev uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(rev); err != nil {
		return err
	}
	hosts := c.selectedLocked()
	if len(hosts) == 0 {
		return fmt.Errorf("select hosts first")
	}
	c.data.State.Plan = nil
	c.data.Approved = false
	return c.startLocked(func(ctx context.Context) error { return c.inspect(ctx, hosts) })
}
func (c *Controller) inspect(ctx context.Context, hosts []Host) error {
	sem := make(chan struct{}, c.opts.Parallelism)
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for _, host := range hosts {
		h := host
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			defer func() { <-sem }()
			c.result(h.ID, "inspecting", "Checking SSH, privileges and Linux facts")
			probeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			facts, err := c.opts.Provisioner.Inspect(probeCtx, transport(h), c.log)
			status, message := "inspected", fmt.Sprintf("%s %s · %s · %d CPUs · %.1f GiB RAM", facts.OSID, facts.OSVersion, facts.Architecture, facts.CPUs, float64(facts.MemoryBytes)/(1<<30))
			if err == nil {
				if storage, ok := c.opts.Provisioner.(interface {
					InspectStorage(context.Context, richt.Host, string) (uint64, error)
				}); ok {
					free, e := storage.InspectStorage(probeCtx, transport(h), h.PoolPath)
					if e != nil {
						err = e
					} else if free == 0 {
						err = fmt.Errorf("no space is available for the selected pool path")
					} else {
						message += fmt.Sprintf("; %.1f GiB available for pool directory", float64(free)/(1<<30))
					}
				}
			}
			if !facts.KVM {
				message += "; KVM is unavailable, QEMU software emulation must be verified"
			}
			if err != nil {
				status = "failed"
				message = err.Error()
				mu.Lock()
				failed++
				mu.Unlock()
			}
			c.mu.Lock()
			if err == nil {
				c.data.Inspections[h.ID] = facts
			} else {
				delete(c.data.Inspections, h.ID)
			}
			for i := range c.data.State.Hosts {
				if c.data.State.Hosts[i].ID == h.ID {
					c.data.State.Hosts[i].Status = status
					c.data.State.Hosts[i].Message = message
				}
			}
			c.mu.Unlock()
			c.result(h.ID, status, message)
		}()
	}
	wg.Wait()
	if failed > 0 {
		return fmt.Errorf("inspection failed for %d host(s); correct the errors or explicitly deselect them", failed)
	}
	return ctx.Err()
}

func (c *Controller) Prepare(rev uint64, stage string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(rev); err != nil {
		return err
	}
	hosts := c.selectedLocked()
	if len(hosts) == 0 {
		return fmt.Errorf("select hosts first")
	}
	if stage != "hosts" && stage != "resources" && stage != "config" {
		return fmt.Errorf("unknown installation stage")
	}
	if stage == "resources" && !c.ready(c.data.HostReady) {
		return fmt.Errorf("complete host preparation first")
	}
	if stage == "config" && !c.ready(c.data.ResourceReady) {
		return fmt.Errorf("complete resource preparation first")
	}
	c.data.State.Plan = nil
	c.data.Approved = false
	c.data.RichtPlan = nil
	c.data.ResourcePlans = nil
	c.data.ConfigDigest = ""
	c.data.State.Phase = stage
	return c.startLocked(func(ctx context.Context) error {
		review := &Review{ID: randomID(), Stage: stage, Summary: []string{}}
		switch stage {
		case "hosts":
			if err := c.inspect(ctx, hosts); err != nil {
				return err
			}
			var targets []richt.Target
			c.mu.Lock()
			for _, h := range hosts {
				facts := c.data.Inspections[h.ID]
				targets = append(targets, richt.Target{Host: transport(h), OSID: facts.OSID, OSVersion: facts.OSVersion, Architecture: facts.Architecture, PoolPaths: []string{h.PoolPath}, RuntimeUser: facts.LoginUser, RuntimeGroup: facts.LoginGroup})
			}
			c.mu.Unlock()
			plan, err := c.opts.Provisioner.Prepare(ctx, targets, c.log)
			if err != nil {
				return err
			}
			review.Output = plan.Summary
			review.Summary = append(review.Summary, "Prepare packages, libvirt access and services on the selected hosts.")
			c.mu.Lock()
			c.data.RichtPlan = plan
			c.mu.Unlock()
		case "resources":
			if err := c.inspect(ctx, hosts); err != nil {
				return err
			}
			service := c.opts.Resources(hosts)
			var plans []*compute.HostPreparationPlan
			for _, h := range hosts {
				plan, err := service.Prepare(ctx, h.ID, resourceSpec(h))
				if err != nil {
					return fmt.Errorf("host %s: %w", h.ID, err)
				}
				if !plan.Before.Network.Exists {
					c.mu.Lock()
					facts := c.data.Inspections[h.ID].Facts
					c.mu.Unlock()
					if err := validateInterfaceConflicts(h.Subnet, facts); err != nil {
						return fmt.Errorf("host %s: %w", h.ID, err)
					}
				}
				plans = append(plans, plan)
				review.Summary = append(review.Summary, plan.Summary...)
			}
			if len(review.Summary) == 0 {
				review.Summary = append(review.Summary, "Selected networks and pools already match; no mutations are required.")
			}
			c.mu.Lock()
			c.data.ResourcePlans = plans
			c.data.ResourceDigest = resourceDigest(hosts, plans)
			c.mu.Unlock()
		case "config":
			if err := c.verifyResources(ctx, hosts); err != nil {
				return err
			}
			c.mu.Lock()
			edits := c.edits()
			c.mu.Unlock()
			candidate, err := c.document.Prepare(edits)
			if err != nil {
				return err
			}
			review.Summary = append(review.Summary, candidate.Summary...)
			review.Summary = append(review.Summary, "Write configuration on the controller: "+c.document.Path(), "Start or restart Vmango yourself after completion; no server service will be changed.")
			c.mu.Lock()
			c.candidate = candidate
			c.data.ConfigDigest = candidate.Digest
			c.mu.Unlock()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.data.State.Plan = review
		return nil
	})
}

func validateInterfaceConflicts(subnet string, data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var facts struct {
		Network struct {
			Interfaces map[string]struct {
				Addresses []struct {
					Address      string `json:"address"`
					PrefixLength int    `json:"prefix_length"`
				} `json:"addresses"`
			} `json:"interfaces"`
		} `json:"network"`
	}
	if err := json.Unmarshal(data, &facts); err != nil {
		return fmt.Errorf("invalid target network facts")
	}
	var prefixes []string
	for _, iface := range facts.Network.Interfaces {
		for _, a := range iface.Addresses {
			addr, err := netip.ParseAddr(a.Address)
			if err != nil {
				return fmt.Errorf("invalid target interface address")
			}
			p := netip.PrefixFrom(addr, a.PrefixLength)
			if !p.IsValid() {
				return fmt.Errorf("invalid target interface prefix")
			}
			prefixes = append(prefixes, p.String())
		}
	}
	return compute.ValidateNATAddressConflicts(subnet, prefixes)
}

func (c *Controller) Apply(rev uint64, planID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(rev); err != nil {
		return err
	}
	if c.data.State.Plan == nil || c.data.State.Plan.ID != planID {
		return fmt.Errorf("review the current prepared plan before applying")
	}
	c.data.Approved = true
	return c.applyLocked()
}
func (c *Controller) Resume(rev uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.check(rev); err != nil {
		return err
	}
	if !c.data.Approved || c.data.State.Plan == nil {
		return fmt.Errorf("no approved interrupted stage is available")
	}
	return c.applyLocked()
}
func (c *Controller) applyLocked() error {
	stage := c.data.State.Plan.Stage
	hosts := c.selectedLocked()
	rp := c.data.RichtPlan
	plans := append([]*compute.HostPreparationPlan{}, c.data.ResourcePlans...)
	candidate := c.candidate
	if stage == "hosts" {
		if rp == nil {
			return fmt.Errorf("saved Richt plan is missing")
		}
		if err := c.opts.Provisioner.VerifyPlan(rp); err != nil {
			return err
		}
	}
	if stage == "config" && (candidate == nil || candidate.Digest != c.data.ConfigDigest) {
		return fmt.Errorf("configuration candidate is missing or changed")
	}
	if stage == "resources" && c.data.ResourceDigest != resourceDigest(hosts, plans) {
		return fmt.Errorf("saved resource plan changed after review; prepare and approve it again")
	}
	return c.startLocked(func(ctx context.Context) error {
		switch stage {
		case "hosts":
			err := c.opts.Provisioner.Apply(ctx, rp, c.log)
			outcomes := map[string]string{}
			if reader, ok := c.opts.Provisioner.(interface {
				Outcomes(*richt.Plan) (map[string]string, error)
			}); ok {
				outcomes, _ = reader.Outcomes(rp)
			}
			for _, h := range hosts {
				success := err == nil || outcomes[h.ID] == "success"
				if success {
					c.mu.Lock()
					c.data.HostReady[h.ID] = hostDigest(h)
					c.mu.Unlock()
					c.result(h.ID, "prepared", "Host preparation completed")
				} else {
					c.result(h.ID, "failed", "Host preparation did not complete; inspect output and retry explicitly")
				}
			}
			if err != nil {
				return err
			}
		case "resources":
			service := c.opts.Resources(hosts)
			var failures []string
			for _, plan := range plans {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := service.Apply(ctx, plan); err != nil {
					failures = append(failures, plan.Node+": "+err.Error())
					c.result(plan.Node, "failed", err.Error())
					continue
				}
				for _, h := range hosts {
					if h.ID == plan.Node {
						c.mu.Lock()
						c.data.ResourceReady[h.ID] = hostDigest(h)
						c.data.ReadyPlans[h.ID] = plan
						c.mu.Unlock()
					}
				}
				c.result(plan.Node, "ready", "Libvirt network and pool verified through the runtime connection")
			}
			if len(failures) > 0 {
				return errors.New(strings.Join(failures, "; "))
			}
		case "config":
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := c.verifyResources(ctx, hosts); err != nil {
				return err
			}
			backup, err := c.document.Publish(candidate)
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.data.State.BackupPath = backup
			c.data.State.Exists = true
			c.data.State.Phase = "complete"
			c.data.State.NeedsAdmin = false
			c.data.PasswordHash = ""
			c.mu.Unlock()
		default:
			return fmt.Errorf("unknown stage")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.data.Approved = false
		c.data.State.CanResume = false
		c.data.State.Plan = nil
		return nil
	})
}

func (c *Controller) verifyResources(ctx context.Context, hosts []Host) error {
	c.mu.Lock()
	api := c.data.State.API
	c.mu.Unlock()
	if err := c.validateAPISelection(api); err != nil {
		return err
	}
	service := c.opts.Resources(hosts)
	for _, h := range hosts {
		plan, err := service.Prepare(ctx, h.ID, resourceSpec(h))
		if err != nil {
			return fmt.Errorf("runtime access for host %s: %w", h.ID, err)
		}
		if len(plan.Actions) > 0 {
			return fmt.Errorf("host %s resources changed; prepare and review the resource stage again", h.ID)
		}
		c.mu.Lock()
		previous := c.data.ReadyPlans[h.ID]
		c.mu.Unlock()
		if previous != nil && ((previous.Spec.NetworkUUID != "" && previous.Spec.NetworkUUID != plan.Spec.NetworkUUID) || (previous.Spec.PoolUUID != "" && previous.Spec.PoolUUID != plan.Spec.PoolUUID)) {
			return fmt.Errorf("host %s resources were replaced after review", h.ID)
		}
	}
	return nil
}

func resourceDigest(hosts []Host, plans []*compute.HostPreparationPlan) string {
	clean := append([]Host{}, hosts...)
	for i := range clean {
		clean[i].Status = ""
		clean[i].Message = ""
	}
	return hash(struct {
		Hosts []Host
		Plans []*compute.HostPreparationPlan
	}{clean, plans})
}

// EqualAnswers is deliberately independent of transient status and selection.
func EqualAnswers(a, b Host) bool {
	a.Selected = false
	a.Status = ""
	a.Message = ""
	b.Selected = false
	b.Status = ""
	b.Message = ""
	return reflect.DeepEqual(a, b)
}
