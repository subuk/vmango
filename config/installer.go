package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl"
	"github.com/hashicorp/hcl/hcl/ast"
	"github.com/hashicorp/hcl/hcl/token"
	"golang.org/x/crypto/bcrypt"
)

// InstallDocument owns one explicitly selected configuration path. Its original
// bytes are authoritative until Publish succeeds. Final symlinks and hardlinks
// are unsupported; parent links are resolved once and checked again on publish.
// The installer must hold its per-configuration lock for this object's lifetime.
type InstallDocument struct {
	path, requested string
	original        []byte
	exists          bool
	info            os.FileInfo
	cfg             *Config
}

type InstallHost struct {
	Cloud, CloudName, Zone, Node, URI              string
	Network, NetworkID, Subnet, DHCPStart, DHCPEnd string
	Pool, PoolPath, StorageClass                   string
}

type InstallAPI struct {
	Listen, BaseURL, TLSCertFile, TLSKeyFile string
	AdminUser, PasswordHash                  string
}

type InstallEdits struct {
	Hosts []InstallHost
	API   *InstallAPI
}

// InstallCandidate deliberately exposes only a safe semantic summary. Bytes
// contains credentials and must never be returned to a browser or logged.
type InstallCandidate struct {
	Summary  []string
	Digest   string
	Changed  bool
	data     []byte
	cfg      *Config
	document *InstallDocument
}

func (d *InstallDocument) Path() string     { return d.path }
func (d *InstallDocument) Exists() bool     { return d.exists }
func (d *InstallDocument) Bytes() []byte    { return append([]byte(nil), d.original...) }
func (d *InstallDocument) Config() *Config  { c, _ := ParseBytes(d.original); return c }
func (c *InstallCandidate) Bytes() []byte   { return append([]byte(nil), c.data...) }
func (c *InstallCandidate) Config() *Config { cfg, _ := ParseBytes(c.data); return cfg }

func OpenInstallDocument(path string) (*InstallDocument, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("an explicit configuration path is required")
	}
	requested, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(requested))
	if err != nil {
		return nil, fmt.Errorf("configuration parent directory: %w", err)
	}
	d := &InstallDocument{path: filepath.Join(parent, filepath.Base(requested)), requested: requested}
	data, info, err := readInstallFile(d.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	d.exists = err == nil
	d.original = data
	d.info = info
	if err := checkInstallWritable(d.path, d.exists); err != nil {
		return nil, err
	}
	d.cfg, err = ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("existing configuration is invalid and will not be replaced: %w", err)
	}
	return d, nil
}

func readInstallFile(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("configuration must be a regular file, not a symlink or special file")
	}
	if err := checkInstallLinks(info); err != nil {
		return nil, nil, err
	}
	if info.Size() > 8<<20 {
		return nil, nil, errors.New("configuration exceeds the 8 MiB installer limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, nil, errors.New("configuration changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > 8<<20 {
		return nil, nil, errors.New("configuration exceeds the 8 MiB installer limit")
	}
	return data, info, nil
}

func installDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (d *InstallDocument) Prepare(edits InstallEdits) (*InstallCandidate, error) {
	data := d.Bytes()
	if len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '{' {
		return nil, errors.New("installer supports native HCL syntax; JSON configuration must be edited manually")
	}
	summary := []string{}
	if !d.exists {
		data = []byte("# Created by vmango install. Activate with the normal server startup.\n")
		if edits.API == nil || edits.API.AdminUser == "" || edits.API.PasswordHash == "" {
			return nil, errors.New("a new configuration requires initial administrator credentials")
		}
	}
	seen := map[string]bool{}
	for _, host := range edits.Hosts {
		if seen[host.Node] {
			return nil, fmt.Errorf("duplicate selected host %q", host.Node)
		}
		seen[host.Node] = true
		if host.Cloud == "" || host.Zone == "" || host.Node == "" || host.URI == "" || host.Network == "" || host.Pool == "" {
			return nil, errors.New("host needs cloud, zone, identifier, URI, network and pool")
		}
		cfg, err := ParseBytes(data)
		if err != nil {
			return nil, err
		}
		var old *CloudHostConfig
		for _, cloud := range cfg.Clouds {
			for _, h := range cloud.Hosts {
				if h.Node == host.Node {
					if cloud.ID != host.Cloud {
						return nil, fmt.Errorf("host %q already belongs to cloud %q", host.Node, cloud.ID)
					}
					copy := h
					old = &copy
				}
			}
		}
		if err := resolveInstallBindings(&host, old); err != nil {
			return nil, err
		}
		before := string(data)
		if old != nil && (old.Uri != host.URI || old.Zone != host.Zone) {
			return nil, fmt.Errorf("existing host %q URI and zone must be preserved; edit them manually if required", host.Node)
		}
		cloudPath := [][]string{{"cloud", host.Cloud}}
		exists, err := installHasBlock(data, cloudPath)
		if err != nil {
			return nil, err
		}
		if !exists {
			name := host.CloudName
			if name == "" {
				name = host.Cloud
			}
			body := "name = " + strconv.Quote(name) + "\ninstance_type \"small\" { cpu = 2 memory_mib = 2048 boot_disk_gib = 8 }\n" + installerImageCatalog
			data, err = installEnsureBlock(data, cloudPath, body)
			if err != nil {
				return nil, err
			}
			summary = append(summary,
				fmt.Sprintf("Cloud %s: create instance type small (2 vCPU, 2048 MiB RAM, 8 GiB boot disk)", host.Cloud),
				fmt.Sprintf("Cloud %s: add pinned Ubuntu 24.04 / 20261017 images for x86_64 with host-default firmware and aarch64 UEFI, using NoCloud initialization", host.Cloud))
		}
		data, err = installEnsureBlock(data, appendPath(cloudPath, []string{"zone", host.Zone}), "")
		if err != nil {
			return nil, err
		}
		data, err = installEnsureBlock(data, appendPath(cloudPath, []string{"network", host.NetworkID}), "")
		if err != nil {
			return nil, err
		}
		data, err = installEnsureBlock(data, appendPath(cloudPath, []string{"storage_class", host.StorageClass}), "")
		if err != nil {
			return nil, err
		}
		hp := appendPath(cloudPath, []string{"host", host.Node})
		if old == nil {
			body := fmt.Sprintf("uri = %q\nzone = %q\nimage_pool = %q\nconfig_drive_pool = %q\ngraphics_listen = \"127.0.0.1\"\nnetwork %q { network = %q }\nstorage_class %q { pool = %q format = \"qcow2\" }\n", host.URI, host.Zone, host.Pool, host.Pool, host.NetworkID, host.Network, host.StorageClass, host.Pool)
			data, err = installEnsureBlock(data, hp, body)
		} else {
			// Existing custom backend settings and resource bindings are never inferred
			// from defaults. Only the explicitly selected logical bindings are edited.
			var network *NetworkBindingConfig
			for _, n := range old.Networks {
				if n.ID == host.NetworkID {
					v := n
					network = &v
				}
			}
			if network != nil && network.Bridge != "" {
				return nil, fmt.Errorf("host %q network %q is a bridge binding; installer NAT conversion is unsupported", host.Node, host.NetworkID)
			}
			np := appendPath(hp, []string{"network", host.NetworkID})
			data, err = installEnsureBlock(data, np, "")
			if err == nil {
				data, err = installSetString(data, np, "network", host.Network)
			}
			if err == nil {
				sp := appendPath(hp, []string{"storage_class", host.StorageClass})
				data, err = installEnsureBlock(data, sp, "")
				if err == nil {
					data, err = installSetString(data, sp, "pool", host.Pool)
				}
			}
		}
		if err != nil {
			return nil, err
		}
		if before != string(data) {
			summary = append(summary, fmt.Sprintf("Configure host %s in cloud %s / zone %s with network %s and pool %s", host.Node, host.Cloud, host.Zone, host.Network, host.Pool))
		}
	}
	if !d.exists {
		clouds := []string{}
		seenCloud := map[string]bool{}
		for _, h := range edits.Hosts {
			if !seenCloud[h.Cloud] {
				seenCloud[h.Cloud] = true
				clouds = append(clouds, strconv.Quote(h.Cloud))
			}
		}
		if len(clouds) == 0 {
			return nil, errors.New("a new configuration requires at least one selected host")
		}
		var err error
		data, err = installEnsureBlock(data, [][]string{{"namespace", "platform"}}, "name = \"Platform\"\nclouds = ["+strings.Join(clouds, ", ")+"]\n")
		if err != nil {
			return nil, err
		}
		summary = append(summary, "Create the Platform namespace with clouds: "+strings.Join(clouds, ", "))
	}
	if edits.API != nil {
		a := edits.API
		if err := validateInstallAPI(*a); err != nil {
			return nil, err
		}
		before := string(data)
		beforeConfig, err := ParseBytes(data)
		if err != nil {
			return nil, err
		}
		p := [][]string{{"api"}}
		data, err = installEnsureBlock(data, p, "")
		if err != nil {
			return nil, err
		}
		values := []struct{ k, v string }{{"listen", a.Listen}, {"base_url", a.BaseURL}, {"tls_cert_file", a.TLSCertFile}, {"tls_key_file", a.TLSKeyFile}}
		for _, v := range values {
			data, err = installSetString(data, p, v.k, v.v)
			if err != nil {
				return nil, err
			}
		}
		if a.PasswordHash != "" {
			if a.AdminUser == "" {
				return nil, errors.New("administrator name is required when changing a password")
			}
			if _, err := bcrypt.Cost([]byte(a.PasswordHash)); err != nil {
				return nil, errors.New("administrator password must already be a bcrypt hash")
			}
			up := [][]string{{"api"}, {"user", a.AdminUser}}
			data, err = installEnsureBlock(data, up, "")
			if err == nil {
				data, err = installSetString(data, up, "hashed_password", a.PasswordHash)
			}
			if err != nil {
				return nil, err
			}
		}
		if before != string(data) {
			previous := beforeConfig.API
			for _, setting := range []struct{ name, old, next string }{
				{"API listener", previous.Listen, a.Listen},
				{"API public URL", installSafeURL(previous.BaseUrl), installSafeURL(a.BaseURL)},
				{"TLS certificate path (controller)", previous.TLSCertFile, a.TLSCertFile},
				{"TLS private-key path (controller)", previous.TLSKeyFile, a.TLSKeyFile},
			} {
				if setting.old != setting.next {
					summary = append(summary, fmt.Sprintf("%s: %q -> %q", setting.name, setting.old, setting.next))
				}
			}
			credentialsChanged := a.PasswordHash != ""
			for _, user := range previous.Users {
				if user.Id == a.AdminUser && user.HashedPassword == a.PasswordHash {
					credentialsChanged = false
				}
			}
			summary = append(summary, fmt.Sprintf("Administrator credentials changed: %t", credentialsChanged))
		}
	}
	cfg, err := ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("candidate configuration is invalid: %w", err)
	}
	return &InstallCandidate{Summary: summary, Digest: installDigest(data), Changed: !bytes.Equal(data, d.original), data: data, cfg: cfg, document: d}, nil
}

// Existing configs may contain an old URL with embedded credentials or query
// tokens. Review shows only its public address, never the raw secret material.
func installSafeURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "(configured URL; value hidden)"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return u.String()
}

// Omitted logical IDs preserve a uniquely identifiable existing binding. They
// must not introduce default catalog entries into an existing custom cloud.
func resolveInstallBindings(host *InstallHost, old *CloudHostConfig) error {
	if old != nil && host.NetworkID == "" {
		matches := []string{}
		for _, binding := range old.Networks {
			if binding.Network == host.Network {
				matches = append(matches, binding.ID)
			}
		}
		if len(matches) == 1 {
			host.NetworkID = matches[0]
		} else if len(matches) == 0 && len(old.Networks) == 1 {
			host.NetworkID = old.Networks[0].ID
		} else if len(old.Networks) > 0 {
			return fmt.Errorf("host %q has ambiguous network bindings; select the logical network explicitly", host.Node)
		}
	}
	if old != nil && host.StorageClass == "" {
		matches := []string{}
		for _, binding := range old.Storage {
			if binding.Pool == host.Pool {
				matches = append(matches, binding.ID)
			}
		}
		if len(matches) == 1 {
			host.StorageClass = matches[0]
		} else if len(matches) == 0 && len(old.Storage) == 1 {
			host.StorageClass = old.Storage[0].ID
		} else if len(old.Storage) > 0 {
			return fmt.Errorf("host %q has ambiguous storage bindings; select the logical storage class explicitly", host.Node)
		}
	}
	if host.NetworkID == "" {
		host.NetworkID = "lan"
	}
	if host.StorageClass == "" {
		host.StorageClass = "standard"
	}
	return nil
}

func validateInstallAPI(a InstallAPI) error {
	host, _, err := net.SplitHostPort(a.Listen)
	if err != nil {
		return fmt.Errorf("API listen must be a host:port address: %w", err)
	}
	u, err := url.Parse(a.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("API base URL must be an absolute HTTP or HTTPS URL")
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback && u.Scheme != "https" {
		return errors.New("non-loopback API access requires an HTTPS public URL")
	}
	return (&APIConfig{TLSCertFile: a.TLSCertFile, TLSKeyFile: a.TLSKeyFile, BaseUrl: a.BaseURL}).ValidateTLS()
}

func appendPath(path [][]string, next []string) [][]string {
	return append(append([][]string(nil), path...), next)
}

func installObject(data []byte, path [][]string) (*ast.ObjectList, *ast.ObjectType, error) {
	f, err := hcl.ParseBytes(data)
	if err != nil {
		return nil, nil, err
	}
	list, ok := f.Node.(*ast.ObjectList)
	if !ok {
		return nil, nil, errors.New("unsupported HCL root")
	}
	var obj *ast.ObjectType
	for _, keys := range path {
		item, err := installItem(list, keys)
		if err != nil {
			return nil, nil, err
		}
		if item == nil {
			return nil, nil, os.ErrNotExist
		}
		obj, ok = item.Val.(*ast.ObjectType)
		if !ok {
			return nil, nil, fmt.Errorf("%s must be an HCL block", strings.Join(keys, " "))
		}
		list = obj.List
	}
	return list, obj, nil
}

func installItem(list *ast.ObjectList, keys []string) (*ast.ObjectItem, error) {
	var found *ast.ObjectItem
	for _, item := range list.Items {
		if len(item.Keys) != len(keys) {
			continue
		}
		match := true
		for i, key := range keys {
			value, ok := item.Keys[i].Token.Value().(string)
			matches := value == key
			if i == 0 {
				matches = strings.EqualFold(value, key)
			}
			if !ok || !matches {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("ambiguous duplicate HCL entry %s; resolve it manually", strings.Join(keys, " "))
		}
		found = item
	}
	return found, nil
}
func installHasBlock(data []byte, path [][]string) (bool, error) {
	_, _, err := installObject(data, path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func installEnsureBlock(data []byte, path [][]string, body string) ([]byte, error) {
	exists, err := installHasBlock(data, path)
	if err != nil || exists {
		return data, err
	}
	_, parent, err := installObject(data, path[:len(path)-1])
	if err != nil {
		return nil, err
	}
	keys := path[len(path)-1]
	name := keys[0]
	for _, label := range keys[1:] {
		name += " " + strconv.Quote(label)
	}
	text := name + " {\n" + body + "}\n"
	return installInsert(data, parent, text), nil
}
func installInsert(data []byte, parent *ast.ObjectType, text string) []byte {
	at := len(data)
	if parent != nil {
		at = parent.Rbrace.Offset
	}
	insertion := "\n" + text
	out := make([]byte, 0, len(data)+len(insertion))
	out = append(out, data[:at]...)
	out = append(out, insertion...)
	out = append(out, data[at:]...)
	return out
}
func installSetString(data []byte, path [][]string, key, value string) ([]byte, error) {
	list, parent, err := installObject(data, path)
	if err != nil {
		return nil, err
	}
	item, err := installItem(list, []string{key})
	if err != nil {
		return nil, err
	}
	if item == nil {
		if value == "" {
			return data, nil
		}
		return installInsert(data, parent, key+" = "+strconv.Quote(value)+"\n"), nil
	}
	literal, ok := item.Val.(*ast.LiteralType)
	if !ok || literal.Token.Type != token.STRING {
		return nil, fmt.Errorf("%s uses unsupported HCL syntax; edit it manually", key)
	}
	if existing, ok := literal.Token.Value().(string); ok && existing == value {
		return data, nil
	}
	start := literal.Token.Pos.Offset
	end := start + len(literal.Token.Text)
	if start < 0 || end > len(data) {
		return nil, errors.New("invalid HCL source offsets")
	}
	result := append([]byte(nil), data[:start]...)
	result = append(result, strconv.Quote(value)...)
	result = append(result, data[end:]...)
	return result, nil
}
