package installer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/installer/richt"

	"golang.org/x/crypto/bcrypt"
)

type fakeProvisioner struct {
	inspect                                             func(context.Context, richt.Host, func(string)) (richt.Inspection, error)
	prepare                                             func(context.Context, []richt.Target, func(string)) (*richt.Plan, error)
	apply                                               func(context.Context, *richt.Plan, func(string)) error
	verify                                              func(*richt.Plan) error
	inspectCalls, prepareCalls, applyCalls, verifyCalls atomic.Int32
	mu                                                  sync.Mutex
	targets                                             []richt.Target
	outcomes                                            map[string]string
}

func (p *fakeProvisioner) Inspect(ctx context.Context, h richt.Host, log func(string)) (richt.Inspection, error) {
	p.inspectCalls.Add(1)
	if p.inspect != nil {
		return p.inspect(ctx, h, log)
	}
	return richt.Inspection{HostID: h.ID, OSID: "almalinux", OSVersion: "10.2", Architecture: "x86_64", LoginUser: "deploy", LoginGroup: "deploy", KVM: false}, nil
}
func (p *fakeProvisioner) Prepare(ctx context.Context, hosts []richt.Target, log func(string)) (*richt.Plan, error) {
	p.prepareCalls.Add(1)
	p.mu.Lock()
	p.targets = append([]richt.Target(nil), hosts...)
	p.mu.Unlock()
	if p.prepare != nil {
		return p.prepare(ctx, hosts, log)
	}
	return &richt.Plan{ID: "prepared-richt-plan", Path: "private-plan.json", Digest: "frozen-inputs", Version: richt.PlanVersion, Summary: "Install selected packages"}, nil
}
func (p *fakeProvisioner) Apply(ctx context.Context, plan *richt.Plan, log func(string)) error {
	p.applyCalls.Add(1)
	if p.apply != nil {
		return p.apply(ctx, plan, log)
	}
	return nil
}
func (p *fakeProvisioner) VerifyPlan(plan *richt.Plan) error {
	p.verifyCalls.Add(1)
	if p.verify != nil {
		return p.verify(plan)
	}
	return nil
}
func (p *fakeProvisioner) Outcomes(*richt.Plan) (map[string]string, error) { return p.outcomes, nil }

type fakeResources struct {
	prepare                  func(context.Context, string, compute.HostPreparationSpec) (*compute.HostPreparationPlan, error)
	apply                    func(context.Context, *compute.HostPreparationPlan) error
	prepareCalls, applyCalls atomic.Int32
}

func (r *fakeResources) Prepare(ctx context.Context, node string, spec compute.HostPreparationSpec) (*compute.HostPreparationPlan, error) {
	r.prepareCalls.Add(1)
	if r.prepare != nil {
		return r.prepare(ctx, node, spec)
	}
	return &compute.HostPreparationPlan{Node: node, Spec: spec, Summary: []string{"Create NAT network and directory pool on " + node}}, nil
}
func (r *fakeResources) Apply(ctx context.Context, plan *compute.HostPreparationPlan) error {
	r.applyCalls.Add(1)
	if r.apply != nil {
		return r.apply(ctx, plan)
	}
	return nil
}

func newTestController(t *testing.T, existing bool, p *fakeProvisioner, r *fakeResources) (*Controller, Options) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "vmango.conf")
	if existing {
		if err := os.WriteFile(path, []byte(`api { listen="127.0.0.1:8080" base_url="http://127.0.0.1:8080" user "owner" { hashed_password="existing-private-hash" } }`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := config.OpenInstallDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		p = &fakeProvisioner{}
	}
	if r == nil {
		r = &fakeResources{}
	}
	opts := Options{Document: doc, StateDir: filepath.Join(root, "state"), Provisioner: p, Resources: func([]Host) Resources { return r }, Parallelism: 2}
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, opts
}
func testAnswers(c *Controller) Answers {
	s := c.Snapshot()
	a := Answers{Revision: s.Revision, API: APISettings{Listen: "127.0.0.1:8080", BaseURL: "http://127.0.0.1:8080", AdminUser: "admin"}, Hosts: []Host{{ID: "test-a", Cloud: "local", Zone: "local", Address: "test-a", User: "deploy", Selected: true, Sudo: true, Network: "default", Subnet: "192.168.122.0/24", DHCPStart: "192.168.122.2", DHCPEnd: "192.168.122.254", Pool: "default", PoolPath: "/var/lib/libvirt/images"}}}
	if s.NeedsAdmin {
		a.API.Password = "private-test-password"
	}
	return a
}
func waitInstaller(t *testing.T, c *Controller) State {
	t.Helper()
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done == nil {
		t.Fatal("operation did not start")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("installer did not finish")
	}
	return c.Snapshot()
}
func prepareStage(t *testing.T, c *Controller, stage string) State {
	t.Helper()
	s := c.Snapshot()
	if err := c.Prepare(s.Revision, stage); err != nil {
		t.Fatal(err)
	}
	s = waitInstaller(t, c)
	if s.Error != "" || s.Plan == nil {
		t.Fatalf("preparation failed: %+v", s)
	}
	return s
}
func applyStage(t *testing.T, c *Controller, stage string) State {
	t.Helper()
	s := prepareStage(t, c, stage)
	if err := c.Apply(s.Revision, s.Plan.ID); err != nil {
		t.Fatal(err)
	}
	s = waitInstaller(t, c)
	if s.Error != "" {
		t.Fatal(s.Error)
	}
	return s
}

func TestControllerPasswordNeverEntersBrowserStateAndIsNotRehashed(t *testing.T) {
	c, opts := newTestController(t, false, nil, nil)
	a := testAnswers(c)
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	passwordHash := c.data.PasswordHash
	c.mu.Unlock()
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(a.API.Password)); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(c.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), a.API.Password) || strings.Contains(string(b), passwordHash) {
		t.Fatal("browser state contains credentials")
	}
	persisted, err := os.ReadFile(filepath.Join(opts.StateDir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), a.API.Password) {
		t.Fatal("recovery state contains plaintext password")
	}
	info, err := os.Stat(filepath.Join(opts.StateDir, "session.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	s := c.Snapshot()
	a.API = s.API
	a.Revision = s.Revision
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	again := c.data.PasswordHash
	c.mu.Unlock()
	if again != passwordHash {
		t.Fatal("unchanged credentials generated a new password hash")
	}
}

func TestControllerEditsInvalidateApprovalAndStagesRequireVerifiedPredecessors(t *testing.T) {
	p := &fakeProvisioner{}
	c, _ := newTestController(t, true, p, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	for _, stage := range []string{"resources", "config"} {
		if err := c.Prepare(s.Revision, stage); err == nil {
			t.Fatalf("allowed %s before prerequisites", stage)
		}
	}
	s = prepareStage(t, c, "hosts")
	oldID, oldRevision := s.Plan.ID, s.Revision
	a := Answers{Revision: s.Revision, Hosts: s.Hosts, API: s.API}
	a.Hosts[0].PoolPath = "/srv/vms"
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Plan != nil {
		t.Fatal("answers retained the old plan")
	}
	if err := c.Apply(oldRevision, oldID); err == nil {
		t.Fatal("accepted stale revision")
	}
	if err := c.Apply(c.Snapshot().Revision, oldID); err == nil {
		t.Fatal("accepted invalidated plan")
	}
	if p.applyCalls.Load() != 0 {
		t.Fatal("executor ran without current approval")
	}
}

func TestControllerDuplicateApplyIsRejectedWhileJobContinues(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	p := &fakeProvisioner{apply: func(ctx context.Context, _ *richt.Plan, _ func(string)) error {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	c, _ := newTestController(t, true, p, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	s := prepareStage(t, c, "hosts")
	if err := c.Apply(s.Revision, s.Plan.ID); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := c.Apply(s.Revision, s.Plan.ID); err == nil {
		t.Fatal("duplicate apply was accepted")
	}
	if !c.Snapshot().Busy {
		t.Fatal("job lost busy state")
	}
	close(release)
	s = waitInstaller(t, c)
	if s.Error != "" || p.applyCalls.Load() != 1 {
		t.Fatal(s.Error, p.applyCalls.Load())
	}
}

func TestControllerExplicitCancellationPreservesResumablePlan(t *testing.T) {
	started := make(chan struct{})
	p := &fakeProvisioner{apply: func(ctx context.Context, _ *richt.Plan, _ func(string)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	c, _ := newTestController(t, true, p, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	s := prepareStage(t, c, "hosts")
	planID := s.Plan.ID
	if err := c.Apply(s.Revision, planID); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := c.Cancel(); err != nil {
		t.Fatal(err)
	}
	s = waitInstaller(t, c)
	if s.Busy || !s.CanResume || s.Plan == nil || s.Plan.ID != planID || s.Error == "" {
		t.Fatalf("cancellation discarded recovery: %+v", s)
	}
	if err := c.Cancel(); err == nil {
		t.Fatal("idle cancellation succeeded")
	}
}

func TestControllerRecoveryResumesExactSavedHostPlanWithoutRepreparing(t *testing.T) {
	p := &fakeProvisioner{apply: func(context.Context, *richt.Plan, func(string)) error { return errors.New("temporary host failure") }}
	c, opts := newTestController(t, true, p, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	s := prepareStage(t, c, "hosts")
	planID := s.Plan.ID
	if err := c.Apply(s.Revision, planID); err != nil {
		t.Fatal(err)
	}
	if s = waitInstaller(t, c); !s.CanResume {
		t.Fatal("failed approved plan is not resumable")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err := config.OpenInstallDocument(opts.Document.Path())
	if err != nil {
		t.Fatal(err)
	}
	opts.Document = doc
	p.apply = nil
	restored, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	s = restored.Snapshot()
	if !s.CanResume || s.Plan == nil || s.Plan.ID != planID {
		t.Fatalf("lost saved plan: %+v", s)
	}
	if err := restored.Resume(s.Revision); err != nil {
		t.Fatal(err)
	}
	s = waitInstaller(t, restored)
	if s.Error != "" || p.prepareCalls.Load() != 1 || p.inspectCalls.Load() != 1 || p.applyCalls.Load() != 2 {
		t.Fatal(s.Error, p.prepareCalls.Load(), p.inspectCalls.Load(), p.applyCalls.Load())
	}
}

func TestControllerConfigConflictPreservesExternalFile(t *testing.T) {
	c, _ := newTestController(t, false, nil, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	applyStage(t, c, "hosts")
	applyStage(t, c, "resources")
	s := prepareStage(t, c, "config")
	external := []byte("# Written by another operator\n")
	if err := os.WriteFile(s.ConfigPath, external, 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Apply(s.Revision, s.Plan.ID); err != nil {
		t.Fatal(err)
	}
	s = waitInstaller(t, c)
	if s.Error == "" || s.Phase == "complete" {
		t.Fatal("configuration conflict was reported as success")
	}
	got, err := os.ReadFile(s.ConfigPath)
	if err != nil || string(got) != string(external) {
		t.Fatal("overwrote external configuration", err)
	}
}

func TestControllerPrivateConfigLockCannotBeBypassedWithAnotherStateDirectory(t *testing.T) {
	_, opts := newTestController(t, true, nil, nil)
	opts.StateDir = filepath.Join(t.TempDir(), "other-state")
	if duplicate, err := New(opts); err == nil {
		duplicate.Close()
		t.Fatal("second state directory bypassed configuration lock")
	}
}

func TestControllerSnapshotDoesNotAllowMutationOfLiveAnswers(t *testing.T) {
	c, _ := newTestController(t, true, nil, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	s.Hosts[0].Address = "replaced"
	s.Clouds[0].Zones[0].ID = "replaced"
	current := c.Snapshot()
	if current.Hosts[0].Address == "replaced" || current.Clouds[0].Zones[0].ID == "replaced" {
		t.Fatal("snapshot aliases live state")
	}
}

func TestControllerFailedHostSubsetRequiresExplicitSelection(t *testing.T) {
	p := &fakeProvisioner{outcomes: map[string]string{"test-a": "success"}, apply: func(context.Context, *richt.Plan, func(string)) error { return errors.New("test-b failed") }}
	c, _ := newTestController(t, true, p, nil)
	a := testAnswers(c)
	b := a.Hosts[0]
	b.ID = "test-b"
	b.Address = "test-b"
	a.Hosts = append(a.Hosts, b)
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	s := prepareStage(t, c, "hosts")
	if err := c.Apply(s.Revision, s.Plan.ID); err != nil {
		t.Fatal(err)
	}
	s = waitInstaller(t, c)
	if err := c.Prepare(s.Revision, "resources"); err == nil {
		t.Fatal("failed host silently treated as prepared")
	}
	a = Answers{Revision: s.Revision, API: s.API, Hosts: s.Hosts}
	a.Hosts[1].Selected = false
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	prepareStage(t, c, "resources")
	if len(c.Snapshot().Hosts) != 2 {
		t.Fatal("unselected host entry was removed")
	}
}

func TestControllerRejectsInvalidResourcesBeforePreparingPackages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Host)
	}{
		{"relative pool directory", func(h *Host) { h.PoolPath = "relative/path" }},
		{"invalid NAT subnet", func(h *Host) { h.Subnet = "192.168.122.17/24" }},
		{"DHCP outside subnet", func(h *Host) { h.DHCPStart = "10.0.0.2" }},
		{"invalid resource name", func(h *Host) { h.Network = "invalid/name" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &fakeProvisioner{}
			c, _ := newTestController(t, true, p, nil)
			a := testAnswers(c)
			tc.change(&a.Hosts[0])
			if err := c.Answers(a); err == nil {
				t.Fatal("invalid resource answer was accepted")
			}
			if p.prepareCalls.Load() != 0 || p.applyCalls.Load() != 0 || len(c.Snapshot().Hosts) != 0 {
				t.Fatal("invalid input changed installation state")
			}
		})
	}
}

func TestControllerRecipeUsesObservedArchitectureAndRuntimeIdentity(t *testing.T) {
	p := &fakeProvisioner{}
	c, _ := newTestController(t, true, p, nil)
	a := testAnswers(c)
	a.Hosts[0].User = ""
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	prepareStage(t, c, "hosts")
	p.mu.Lock()
	targets := append([]richt.Target(nil), p.targets...)
	p.mu.Unlock()
	if len(targets) != 1 || targets[0].Architecture != "x86_64" || targets[0].RuntimeUser != "deploy" || targets[0].RuntimeGroup != "deploy" {
		t.Fatalf("recipe did not use target facts: %+v", targets)
	}
}

func TestControllerRecoversPublicationBeforeProgressWasSaved(t *testing.T) {
	c, opts := newTestController(t, false, nil, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	applyStage(t, c, "hosts")
	applyStage(t, c, "resources")
	prepareStage(t, c, "config")
	c.mu.Lock()
	c.data.Approved = true
	err := c.persistLocked()
	candidate := c.candidate
	c.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.document.Publish(candidate); err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(c.document.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err := config.OpenInstallDocument(c.document.Path())
	if err != nil {
		t.Fatal(err)
	}
	opts.Document = doc
	restored, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	s := restored.Snapshot()
	if s.Phase != "complete" || s.CanResume || s.Plan != nil {
		t.Fatalf("published configuration was not recognized: %+v", s)
	}
	after, err := os.ReadFile(c.document.Path())
	if err != nil || string(after) != string(published) {
		t.Fatal("recovery rewrote configuration", err)
	}
}

func TestControllerCompletedPublicationRejectsFurtherWork(t *testing.T) {
	c, _ := newTestController(t, false, nil, nil)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	applyStage(t, c, "hosts")
	applyStage(t, c, "resources")
	s := applyStage(t, c, "config")
	if s.Phase != "complete" {
		t.Fatalf("publication did not complete: %+v", s)
	}
	for name, action := range map[string]func() error{
		"answers":  func() error { return c.Answers(testAnswers(c)) },
		"inspect":  func() error { return c.Inspect(s.Revision) },
		"discover": func() error { return c.Discover(s.Revision) },
		"prepare":  func() error { return c.Prepare(s.Revision, "hosts") },
		"apply":    func() error { return c.Apply(s.Revision, "old-plan") },
		"resume":   func() error { return c.Resume(s.Revision) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil {
				t.Fatal("completed session accepted more work")
			}
			current := c.Snapshot()
			if current.Phase != "complete" || current.Busy || current.Revision != s.Revision {
				t.Fatalf("completed session changed: %+v", current)
			}
		})
	}
}

func TestControllerRechecksResourceReadinessBeforeConfigPublication(t *testing.T) {
	changed := false
	r := &fakeResources{prepare: func(_ context.Context, node string, spec compute.HostPreparationSpec) (*compute.HostPreparationPlan, error) {
		if changed {
			return nil, errors.New("runtime access was revoked after review")
		}
		return &compute.HostPreparationPlan{Node: node, Spec: spec}, nil
	}}
	c, _ := newTestController(t, false, nil, r)
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	applyStage(t, c, "hosts")
	applyStage(t, c, "resources")
	s := prepareStage(t, c, "config")
	changed = true
	if err := c.Apply(s.Revision, s.Plan.ID); err != nil {
		t.Fatal(err)
	}
	s = waitInstaller(t, c)
	if s.Error == "" || s.Phase == "complete" {
		t.Fatal("stale runtime access was accepted for config publication")
	}
	if _, err := os.Stat(c.document.Path()); !os.IsNotExist(err) {
		t.Fatal("config was published despite failed runtime recheck", err)
	}
}

func TestControllerRejectsReplacedResourcePlansBeforeApplyOrRecovery(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "current session", true: "recovered session"}[recovery], func(t *testing.T) {
			r := &fakeResources{}
			c, opts := newTestController(t, true, nil, r)
			if err := c.Answers(testAnswers(c)); err != nil {
				t.Fatal(err)
			}
			applyStage(t, c, "hosts")
			s := prepareStage(t, c, "resources")
			if !recovery {
				c.mu.Lock()
				c.data.ResourcePlans[0].Spec.PoolPath = "/different/unreviewed/path"
				c.mu.Unlock()
				if err := c.Apply(s.Revision, s.Plan.ID); err == nil {
					t.Fatal("replaced resource plan was accepted")
				}
			} else {
				c.mu.Lock()
				c.data.Approved = true
				err := c.persistLocked()
				c.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				if err = c.Close(); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(opts.StateDir, "session.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var replaced saved
				if err = json.Unmarshal(data, &replaced); err != nil {
					t.Fatal(err)
				}
				replaced.ResourcePlans[0].Spec.PoolPath = "/different/unreviewed/path"
				data, err = json.Marshal(replaced)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				restored, err := New(opts)
				if err == nil {
					defer restored.Close()
					if err = restored.Resume(restored.Snapshot().Revision); err == nil {
						t.Fatal("replaced saved resource plan was resumed")
					}
				}
			}
			if r.applyCalls.Load() != 0 {
				t.Fatal("unreviewed resource mutation reached executor")
			}
		})
	}
}
