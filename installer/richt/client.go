// Package richt integrates the separately distributed Richt executable.
package richt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

const PlanVersion = "1.8"
const maxOutput = 4 << 20

type Options struct {
	Binary      string
	StateDir    string
	Parallelism int
}

type Host struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	User    string `json:"user"`
	Port    int    `json:"port"`
	Local   bool   `json:"local"`
	Sudo    bool   `json:"sudo"`
}

type Target struct {
	Host
	OSID         string   `json:"os_id"`
	OSVersion    string   `json:"os_version"`
	Architecture string   `json:"architecture"`
	PoolPaths    []string `json:"pool_paths"`
	RuntimeUser  string   `json:"runtime_user"`
	RuntimeGroup string   `json:"runtime_group"`
}

type Inspection struct {
	HostID        string          `json:"host_id"`
	OSID          string          `json:"os_id"`
	OSVersion     string          `json:"os_version"`
	Architecture  string          `json:"architecture"`
	Hostname      string          `json:"hostname"`
	CPUs          int             `json:"cpus"`
	MemoryBytes   uint64          `json:"memory_bytes"`
	KVM           bool            `json:"kvm"`
	LoginUser     string          `json:"login_user"`
	LoginGroup    string          `json:"login_group"`
	Facts         json.RawMessage `json:"facts"`
	Prerequisites []string        `json:"prerequisites"`
}

// Plan is private recovery metadata. It must never be sent as a browser response.
type Plan struct {
	ID                string            `json:"id"`
	Path              string            `json:"path"`
	Digest            string            `json:"digest"`
	Summary           string            `json:"summary"`
	Version           string            `json:"version"`
	BinaryDigest      string            `json:"binary_digest"`
	ExecutableVersion string            `json:"executable_version"`
	RecipeRevision    string            `json:"recipe_revision"`
	RecipeDigests     map[string]string `json:"recipe_digests"`
	Hosts             []string          `json:"hosts"`
}

type Client struct {
	binary, binaryDigest, stateDir, wrapperDir string
	executableVersion                          string
	parallelism                                int
}

func New(o Options) (*Client, error) {
	if o.StateDir == "" {
		return nil, errors.New("Richt needs a private installation state directory")
	}
	abs, err := filepath.Abs(o.StateDir)
	if err != nil {
		return nil, err
	}
	if err := privateDirectory(abs); err != nil {
		return nil, err
	}
	if o.Parallelism == 0 {
		o.Parallelism = 2
	}
	if o.Parallelism < 1 || o.Parallelism > 8 {
		return nil, errors.New("installer parallelism must be 1..8")
	}
	c := &Client{stateDir: abs, parallelism: o.Parallelism}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c.binary, c.binaryDigest, err = discover(ctx, o.Binary, filepath.Join(abs, "runtime"))
	if err != nil {
		return nil, err
	}
	// Richt inherits a private PATH wrapper, enforcing host-key verification even
	// when the user's general SSH configuration permits unknown host keys.
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return nil, errors.New("OpenSSH is required on the controller")
	}
	ssh, err = filepath.Abs(ssh)
	if err != nil {
		return nil, err
	}
	c.wrapperDir = filepath.Join(abs, "transport")
	if err := privateDirectory(c.wrapperDir); err != nil {
		return nil, err
	}
	wrapper := "#!/bin/sh\nexec " + shellQuote(ssh) + " -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 \"$@\"\n"
	if err := os.WriteFile(filepath.Join(c.wrapperDir, "ssh"), []byte(wrapper), 0700); err != nil {
		return nil, err
	}
	out, err := c.run(ctx, nil, nil, "--version")
	if err != nil {
		return nil, fmt.Errorf("could not query Richt executable version: %w", err)
	}
	c.executableVersion = strings.TrimPrefix(strings.TrimSpace(string(out)), "richt ")
	return c, nil
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("installation directory %s must be private (0700) and not a symlink", path)
	}
	return nil
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var username = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}\$?$`)

func (h Host) Validate() error {
	if !identifier.MatchString(h.ID) {
		return errors.New("host identifier must contain letters, digits, hyphens or underscores")
	}
	if h.Port < 0 || h.Port > 65535 {
		return errors.New("SSH port must be 1..65535 or omitted")
	}
	if h.Local {
		if runtime.GOOS != "linux" {
			return errors.New("local hypervisor preparation requires Linux")
		}
		if h.Address != "" || h.User != "" || h.Port != 0 {
			return errors.New("local hosts cannot specify SSH connection settings")
		}
		return nil
	}
	if h.Address == "" || strings.HasPrefix(h.Address, "-") || strings.ContainsAny(h.Address, "/@ \t\r\n\x00") {
		return errors.New("SSH destination must be a hostname, IP address or SSH alias")
	}
	if h.User != "" && !username.MatchString(h.User) {
		return errors.New("invalid SSH user")
	}
	return nil
}

func transport(h Host) map[string]any {
	t := map[string]any{"type": "ssh", "host": h.Address, "sudo": h.Sudo}
	if h.Local {
		t = map[string]any{"type": "local", "sudo": h.Sudo}
	}
	if h.User != "" {
		t["user"] = h.User
	}
	if h.Port != 0 {
		t["port"] = h.Port
	}
	return t
}

func (c *Client) newDirectory(prefix string) (string, error) {
	return os.MkdirTemp(c.stateDir, prefix+"-")
}

func writeYAML(path string, v any) error {
	b, e := yaml.Marshal(v)
	if e != nil {
		return e
	}
	return os.WriteFile(path, b, 0600)
}

func (c *Client) Inspect(ctx context.Context, h Host, log func(string)) (Inspection, error) {
	r := Inspection{HostID: h.ID, Prerequisites: []string{}}
	if err := h.Validate(); err != nil {
		return r, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	// Probe only prerequisites here. No packages, accounts or services are changed.
	probe := `set -eu
test "$(uname -s)" = Linux || { echo 'Linux target required'; exit 1; }
for tool in python3 ip systemctl getent; do command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool"; exit 1; }; done
test -d /run/systemd/system || { echo 'A running systemd system is required'; exit 1; }
id -un
id -gn
if test -r /dev/kvm && test -w /dev/kvm; then echo kvm; else echo no-kvm; fi
`
	out, err := c.target(ctx, h, []byte(probe), false, "sh", "-s")
	if err != nil {
		return r, fmt.Errorf("host prerequisite check failed: %s: %w", safeText(strings.TrimSpace(string(out))), err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		return r, errors.New("unexpected host prerequisite response")
	}
	r.LoginUser, r.LoginGroup, r.KVM = lines[0], lines[1], lines[2] == "kvm"
	if h.Sudo {
		if _, err := c.target(ctx, h, nil, true, "true"); err != nil {
			return r, errors.New("explicit sudo -n privilege check failed")
		}
	} else if r.LoginUser != "root" {
		return r, errors.New("hypervisor preparation needs root or explicitly enabled sudo -n")
	}
	dir, err := c.newDirectory("inspect")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(dir)
	inv := filepath.Join(dir, "inventory.yaml")
	if err := writeYAML(inv, map[string]any{"hosts": []any{map[string]any{"name": h.ID, "transport": transport(h)}}}); err != nil {
		return r, err
	}
	out, err = c.run(ctx, nil, log, "facts", "-i", inv, "--refresh", "--no-facts-cache", "--progress=plain", "--parallelism=1")
	if err != nil {
		return r, fmt.Errorf("fresh Linux facts failed: %w", err)
	}
	var env struct {
		SchemaVersion int `json:"schema_version"`
		Hosts         []struct {
			Host  string          `json:"host"`
			Error string          `json:"error"`
			Facts json.RawMessage `json:"facts"`
		} `json:"hosts"`
	}
	if json.Unmarshal(out, &env) != nil || env.SchemaVersion != 1 || len(env.Hosts) != 1 || env.Hosts[0].Host != h.ID || env.Hosts[0].Error != "" {
		return r, errors.New("Richt returned incompatible or failed facts")
	}
	var facts struct {
		OS struct {
			ID      string `json:"id"`
			Version string `json:"version_id"`
		} `json:"os"`
		Architecture string `json:"architecture"`
		Hostname     string `json:"hostname"`
		CPU          struct {
			Count int `json:"logical_count"`
		} `json:"cpu"`
		Memory struct {
			Total uint64 `json:"total_bytes"`
		} `json:"memory"`
	}
	if json.Unmarshal(env.Hosts[0].Facts, &facts) != nil {
		return r, errors.New("Richt facts are malformed")
	}
	r.OSID, r.OSVersion, r.Architecture, r.Hostname, r.CPUs, r.MemoryBytes, r.Facts = facts.OS.ID, facts.OS.Version, facts.Architecture, facts.Hostname, facts.CPU.Count, facts.Memory.Total, env.Hosts[0].Facts
	if _, err := recipeFor(r.OSID, r.OSVersion, r.Architecture); err != nil {
		return r, err
	}
	r.Prerequisites = []string{"OpenSSH host trust and connectivity verified", "Linux/systemd, Python and iproute2 available", "Preparation privileges verified"}
	return r, nil
}

func (c *Client) target(ctx context.Context, h Host, input []byte, elevate bool, name string, args ...string) ([]byte, error) {
	if elevate && h.Sudo {
		args = append([]string{"-n", "--", name}, args...)
		name = "sudo"
	}
	if h.Local {
		return execute(ctx, name, args, input, nil, nil)
	}
	remote := []string{name}
	remote = append(remote, args...)
	for i := range remote {
		remote[i] = shellQuote(remote[i])
	}
	a := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=15", "-o", "RequestTTY=no", "-o", "RemoteCommand=none", "-o", "SessionType=default", "-o", "StdinNull=no", "-o", "ForkAfterAuthentication=no"}
	if h.User != "" {
		a = append(a, "-l", h.User)
	}
	if h.Port != 0 {
		a = append(a, "-p", strconv.Itoa(h.Port))
	}
	a = append(a, "--", h.Address, "exec "+strings.Join(remote, " "))
	return execute(ctx, "ssh", a, input, nil, nil)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func (c *Client) run(ctx context.Context, input []byte, log func(string), args ...string) ([]byte, error) {
	if err := verifyExecutable(c.binary, c.binaryDigest); err != nil {
		return nil, err
	}
	env := append([]string{}, os.Environ()...)
	for i, s := range env {
		if strings.HasPrefix(s, "PATH=") {
			env[i] = "PATH=" + c.wrapperDir + string(os.PathListSeparator) + strings.TrimPrefix(s, "PATH=")
		}
	}
	env = append(env, "SSH_ASKPASS_REQUIRE=never")
	return execute(ctx, c.binary, args, input, env, log)
}

type limitedOutput struct {
	mu        sync.Mutex
	b         bytes.Buffer
	log       func(string)
	pending   string
	truncated bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	left := maxOutput - w.b.Len()
	if left <= 0 {
		w.truncated = true
		return n, nil
	}
	if len(p) > left {
		p = p[:left]
		w.truncated = true
	}
	w.b.Write(p)
	if w.log != nil {
		w.pending += string(p)
		for {
			i := strings.IndexByte(w.pending, '\n')
			if i < 0 {
				break
			}
			w.log(safeText(w.pending[:i]))
			w.pending = w.pending[i+1:]
		}
	}
	return n, nil
}
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r >= 32 && r != 127 {
			return r
		}
		return -1
	}, s)
}

func execute(ctx context.Context, name string, args []string, input []byte, env []string, log func(string)) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	if env != nil {
		cmd.Env = env
	}
	cmd.WaitDelay = 3 * time.Second
	stopCancellation := configureCancellation(cmd)
	defer stopCancellation()
	out := &limitedOutput{}
	errout := &limitedOutput{log: log}
	cmd.Stdout = out
	cmd.Stderr = errout
	err := cmd.Run()
	if log != nil && errout.pending != "" {
		log(safeText(errout.pending))
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("process interrupted; re-observe remote state before retrying: %w", ctx.Err())
	}
	if out.truncated || errout.truncated {
		return nil, errors.New("subprocess output exceeded the installer limit")
	}
	if err != nil {
		return out.b.Bytes(), fmt.Errorf("%s failed: %w", filepath.Base(name), err)
	}
	return out.b.Bytes(), nil
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func verifyExecutable(path, expected string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 {
		return errors.New("Richt must be a regular executable not writable by group or others")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, 128<<20)); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return errors.New("Richt executable checksum changed during the installation session")
	}
	return nil
}
