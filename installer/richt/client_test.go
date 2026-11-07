//go:build linux || darwin

package richt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v2"
)

func fakeClient(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "richt")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then printf '%s\\n' 'richt dev'; exit 0; fi\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{StateDir: filepath.Join(dir, "state"), Binary: path})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDiscoveryDetectsExecutableReplacement(t *testing.T) {
	c := fakeClient(t)
	if err := os.WriteFile(c.binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := c.run(context.Background(), nil, nil, "--version"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("changed executable accepted: %v", err)
	}
}

func TestAcceptsDifferentExecutableVersions(t *testing.T) {
	for _, version := range []string{"dev", "v2.0.0", strings.Repeat("a", 40)} {
		t.Run(version, func(t *testing.T) {
			c := fakeClient(t)
			script := "#!/bin/sh\nprintf '%s\\n' " + shellQuote("richt "+version) + "\n"
			if err := os.WriteFile(c.binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			c, err := New(Options{StateDir: c.stateDir, Binary: c.binary})
			if err != nil {
				t.Fatal(err)
			}
			if c.executableVersion != version {
				t.Fatalf("version = %q, want %q", c.executableVersion, version)
			}
			p := writeTestPlan(t, c, initialPlan)
			if err := c.VerifyPlan(p); err != nil {
				t.Fatalf("plan rejected for version %q: %v", version, err)
			}
			p.BinaryDigest = strings.Repeat("0", 64)
			if err := c.VerifyPlan(p); err == nil {
				t.Fatal("plan prepared with another executable accepted")
			}
		})
	}
}

func TestRejectsFailedVersionCommand(t *testing.T) {
	c := fakeClient(t)
	if err := os.WriteFile(c.binary, []byte("#!/bin/sh\nexit 23\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{StateDir: c.stateDir, Binary: c.binary}); err == nil || !strings.Contains(err.Error(), "could not query Richt executable version") {
		t.Fatalf("failed version command accepted or misleading error: %v", err)
	}
}

func TestPrivateStateAndHostValidation(t *testing.T) {
	dir := t.TempDir()
	_ = os.Chmod(dir, 0755)
	if _, err := New(Options{StateDir: dir}); err == nil {
		t.Fatal("public state directory accepted")
	}
	for _, h := range []Host{{ID: "bad id", Address: "host"}, {ID: "h", Address: "-oProxyCommand=x"}, {ID: "h", Address: "a@b"}, {ID: "h", Address: "a\nb"}, {ID: "h", Address: "host", User: "-lroot"}, {ID: "h", Address: "host", Port: 65536}} {
		if h.Validate() == nil {
			t.Errorf("invalid host accepted: %+v", h)
		}
	}
	if err := (Host{ID: "host-1", Address: "host-alias", User: "almalinux", Port: 2222}).Validate(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" && (Host{ID: "local", Local: true}).Validate() == nil {
		t.Fatal("non-Linux local preparation accepted")
	}
}

func TestProcessFailureLimitsAndCancellation(t *testing.T) {
	out, err := execute(context.Background(), "sh", []string{"-c", "printf useful; printf private-secret >&2; exit 23"}, nil, nil, nil)
	if err == nil || !strings.Contains(string(out), "useful") || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unsafe process outcome: %s %v", out, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = execute(ctx, "sh", []string{"-c", "sleep 30"}, nil, nil, nil); err == nil {
		t.Fatal("cancellation ignored")
	}
	if time.Since(start) > 7*time.Second {
		t.Fatal("canceled subprocess did not terminate")
	}
	w := &limitedOutput{}
	_, _ = w.Write([]byte(strings.Repeat("x", maxOutput+1)))
	if !w.truncated || w.b.Len() != maxOutput {
		t.Fatal("unbounded subprocess capture")
	}
}

func writeTestPlan(t *testing.T, c *Client, body string) *Plan {
	t.Helper()
	dir, err := c.newDirectory("plan")
	if err != nil {
		t.Fatal(err)
	}
	p := &Plan{ID: filepath.Base(dir), Path: filepath.Join(dir, "plan.json"), Version: PlanVersion, BinaryDigest: c.binaryDigest, ExecutableVersion: c.executableVersion, Hosts: []string{"a", "b"}}
	if err := os.WriteFile(p.Path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	p.Digest, _, err = readPlan(p.Path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const initialPlan = `{"version":"1.8","hosts":[{"name":"a","transport":"ssh","connection":{"host":"a","sudo":true},"tasks":[{"id":"a/0","service":"hypervisor","component":"binary.qemu","operation":"rpm","rpm":{"package":"qemu-kvm","state":"present"},"progress":{"status":"pending"}}]},{"name":"b","transport":"ssh","tasks":[]}]}`

func TestApprovalPreservesInputsAcrossProgressAndRejectsChanges(t *testing.T) {
	c := fakeClient(t)
	p := writeTestPlan(t, c, initialPlan)
	if err := c.VerifyPlan(p); err != nil {
		t.Fatal(err)
	}
	completed := strings.Replace(initialPlan, `"status":"pending"`, `"status":"success"`, 1)
	_ = os.WriteFile(p.Path, []byte(completed), 0600)
	if err := c.VerifyPlan(p); err != nil {
		t.Fatalf("normal progress invalidated approval: %v", err)
	}
	out, err := c.Outcomes(p)
	if err != nil || out["a"] != "success" || out["b"] != "success" {
		t.Fatalf("outcomes: %v %v", out, err)
	}
	failed := strings.Replace(initialPlan, `"status":"pending"`, `"status":"error","error":"private remote diagnostic"`, 1)
	_ = os.WriteFile(p.Path, []byte(failed), 0600)
	out, err = c.Outcomes(p)
	if err != nil || out["a"] != "failed" || out["b"] != "success" {
		t.Fatalf("partial outcomes: %v %v", out, err)
	}
	changed := strings.Replace(completed, `"package":"qemu-kvm"`, `"package":"other-package"`, 1)
	_ = os.WriteFile(p.Path, []byte(changed), 0600)
	if err := c.Apply(context.Background(), p, nil); err == nil || !strings.Contains(err.Error(), "changed after review") {
		t.Fatalf("unapproved operation accepted: %v", err)
	}
}

func TestPlanVersionPathsAndPermissions(t *testing.T) {
	c := fakeClient(t)
	p := writeTestPlan(t, c, initialPlan)
	_ = os.Chmod(p.Path, 0644)
	if c.VerifyPlan(p) == nil {
		t.Fatal("public plan accepted")
	}
	_ = os.Chmod(p.Path, 0600)
	p.Path = filepath.Join(t.TempDir(), "plan.json")
	_ = os.WriteFile(p.Path, []byte(initialPlan), 0600)
	if c.VerifyPlan(p) == nil {
		t.Fatal("outside plan accepted")
	}
	if _, _, err := readPlanVersionForTest(t, strings.Replace(initialPlan, `"1.8"`, `"1.7"`, 1)); err == nil {
		t.Fatal("incompatible plan accepted")
	}
}

func readPlanVersionForTest(t *testing.T, body string) (string, []savedHost, error) {
	p := filepath.Join(t.TempDir(), "plan.json")
	_ = os.WriteFile(p, []byte(body), 0600)
	return readPlan(p)
}

func TestInventoryPinsRecipesAndIsolatesHosts(t *testing.T) {
	targets := []Target{{Host: Host{ID: "a", Address: "host-a", User: "almalinux", Sudo: true}, OSID: "almalinux", OSVersion: "10.2", Architecture: "x86_64", RuntimeUser: "almalinux", RuntimeGroup: "almalinux", PoolPaths: []string{"/var/lib/vmango-pool"}}, {Host: Host{ID: "b", Address: "host-b", User: "root"}, OSID: "ubuntu", OSVersion: "24.04", Architecture: "aarch64", PoolPaths: []string{"/srv/vm-images"}}}
	inv, pins, err := inventoryAtRevision(targets, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	b, err := yaml.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"https://raw.githubusercontent.com/subuk/vmango/" + strings.Repeat("a", 40), "sha256:", "append_groups: true", "/srv/vm-images", "ubuntu-24.04-arm64.yaml"} {
		if !strings.Contains(string(b), part) {
			t.Fatalf("missing %s in inventory", part)
		}
	}
	if len(pins) != 2 {
		t.Fatal("missing recipe pins")
	}
	for name := range inv["services"].(map[string]any) {
		if !identifier.MatchString(name) {
			t.Fatalf("invalid generated Richt service identifier %q", name)
		}
	}
	for _, sum := range pins {
		if len(sum) != 64 {
			t.Fatal("invalid recipe digest")
		}
	}
	targets[1].ID = "a"
	if _, _, err := inventoryAtRevision(targets, strings.Repeat("a", 40)); err == nil {
		t.Fatal("duplicate host accepted")
	}
}

func TestPlanDigestDoesNotIgnoreNestedOperationProgress(t *testing.T) {
	c := fakeClient(t)
	var value map[string]any
	if err := json.Unmarshal([]byte(initialPlan), &value); err != nil {
		t.Fatal(err)
	}
	host := value["hosts"].([]any)[0].(map[string]any)
	task := host["tasks"].([]any)[0].(map[string]any)
	task["rpm"].(map[string]any)["progress"] = "original"
	data, _ := json.Marshal(value)
	p := writeTestPlan(t, c, string(data))
	task["rpm"].(map[string]any)["progress"] = "changed"
	data, _ = json.Marshal(value)
	_ = os.WriteFile(p.Path, data, 0600)
	if c.VerifyPlan(p) == nil {
		t.Fatal("executable field incorrectly excluded from digest")
	}
}

func TestApplyUsesOnlySavedPlanAndRequiresCompletion(t *testing.T) {
	c := fakeClient(t)
	p := writeTestPlan(t, c, initialPlan)
	capture := filepath.Join(c.stateDir, "arguments")
	completed := strings.Replace(initialPlan, `"status":"pending"`, `"status":"success"`, 1)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(capture) + "\ncat > " + shellQuote(p.Path) + " <<'PLAN_BYTES'\n" + completed + "\nPLAN_BYTES\n"
	if err := os.WriteFile(c.binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c.binaryDigest = digest([]byte(script))
	p.BinaryDigest = c.binaryDigest
	if err := c.Apply(context.Background(), p, nil); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := "apply\n-y\n--progress=plain\n--parallelism=2\n" + p.Path + "\n"
	if string(args) != want {
		t.Fatalf("apply inputs changed: %q", args)
	}
	if err := os.WriteFile(p.Path, []byte(initialPlan), 0600); err != nil {
		t.Fatal(err)
	}
	script = "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(c.binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c.binaryDigest = digest([]byte(script))
	p.BinaryDigest = c.binaryDigest
	if err := c.Apply(context.Background(), p, nil); err == nil || !strings.Contains(err.Error(), "final outcome") {
		t.Fatalf("partial progress accepted as success: %v", err)
	}
}
