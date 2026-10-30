package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"subuk/vmango/auth"
	"subuk/vmango/filesystem"
	"testing"
	"time"
)

type observedRepository struct {
	auth.Repository
	reads, lists int
	failWrites   bool
}

var storageFailure = errors.New("simulated durable commit failure")

func (r *observedRepository) Create(rec auth.Record, limit int) error {
	if r.failWrites {
		return storageFailure
	}
	return r.Repository.Create(rec, limit)
}
func (r *observedRepository) List() ([]auth.Record, error) { r.lists++; return r.Repository.List() }
func (r *observedRepository) Update(id string, fn func(*auth.Record) (bool, error)) error {
	r.reads++
	return r.Repository.Update(id, func(rec *auth.Record) (bool, error) {
		changed, err := fn(rec)
		if err == nil && changed && r.failWrites {
			return false, storageFailure
		}
		return changed, err
	})
}

func TestNoAuthorizationCacheAndCommitBeforeIssue(t *testing.T) {
	base := setup(t)
	sessions := &observedRepository{Repository: base.sessions}
	tokens := &observedRepository{Repository: base.tokens}
	s, err := auth.New(signingKey, sessions, tokens, func(auth.Principal) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if sessions.lists+tokens.lists+sessions.reads+tokens.reads != 0 {
		t.Fatal("registry was preloaded")
	}
	web, err := s.WebToken(principal("admin"))
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err = s.Authenticate(web); err != nil {
			t.Fatal(err)
		}
	}
	if sessions.reads+tokens.reads != 0 {
		t.Fatal("web authorization touched a credential store")
	}
	c, err := s.Authenticate(web)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := s.CreateToken(c, "CI", 90)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(raw); err != nil {
		t.Fatal(err)
	}
	before := tokens.reads
	if _, err = s.Authenticate(raw); err != nil {
		t.Fatal(err)
	}
	if tokens.reads != before+1 {
		t.Fatal("personal authentication did not read exactly one fresh record after usage update", before, tokens.reads)
	}
	sessions.failWrites = true
	access, refresh, _, err := s.Login(principal("admin"))
	if !errors.Is(err, storageFailure) || access != "" || refresh != "" {
		t.Fatal("failed commit issued credentials", err)
	}
	sessions.failWrites = false
	_, refresh, _, err = s.Login(principal("admin"))
	if err != nil {
		t.Fatal(err)
	}
	sessions.failWrites = true
	access, next, _, err := s.Refresh(refresh)
	if !errors.Is(err, storageFailure) || access != "" || next != "" {
		t.Fatal("failed rotation issued credentials", err)
	}
	sessions.failWrites = false
	if _, _, _, err = s.Refresh(refresh); err != nil {
		t.Fatal("failed pre-commit rotation damaged the previous durable credential", err)
	}
	tokens.failWrites = true
	if _, raw, err = s.CreateToken(c, "failed", 90); !errors.Is(err, storageFailure) || raw != "" {
		t.Fatal("failed token persistence issued a bearer", err)
	}
}

func TestPrivateStoreRejectsUnsafeFilesAndCleansExpiredRecords(t *testing.T) {
	base := setup(t)
	c := interactive(t, base.Service, "admin")
	m, _, err := base.CreateToken(c, "CI", 90)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base.dir, "tokens", m.ID+".json")
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(file)
		if info.Mode().Perm() != 0600 {
			t.Fatal("token file is not private")
		}
		if err = os.Chmod(file, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err = base.tokens.List(); err == nil {
			t.Fatal("public token metadata permissions accepted")
		}
		if err = os.Chmod(file, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = base.tokens.Update(m.ID, func(r *auth.Record) (bool, error) {
		r.CreatedAt = time.Now().Add(-48 * time.Hour)
		r.ExpiresAt = time.Now().Add(-time.Hour)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = base.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("expired record remains", err)
	}
	if runtime.GOOS != "windows" {
		if err = os.Symlink(filepath.Join(base.dir, "outside"), file); err != nil {
			t.Fatal(err)
		}
		if _, err = base.tokens.List(); err == nil {
			t.Fatal("symlink token record accepted")
		}
	}
}

func TestIdentityEligibilityRequiresVerifiedEmailAndStableProvider(t *testing.T) {
	p := auth.IdentityPolicy{Users: []auth.LocalUser{{ID: "admin"}}, OIDCIssuer: "https://provider.test", OIDCClientID: "client", AllowedEmails: []string{"same@example.com"}}
	oidc := auth.Principal{Method: "oidc", Issuer: p.OIDCIssuer, Subject: "subject", Email: "same@example.com"}
	if p.Eligible(oidc) {
		t.Fatal("unverified email passed allowlist")
	}
	oidc.EmailVerified = true
	if !p.Eligible(oidc) {
		t.Fatal("verified identity rejected")
	}
	oidc.Issuer = "https://other.test"
	if p.Eligible(oidc) {
		t.Fatal("provider change transferred credentials")
	}
	if !p.Eligible(principal("admin")) || p.Eligible(principal("removed")) {
		t.Fatal("configured local eligibility ignored")
	}
	// Unknown versions fail startup rather than being recreated as active records.
	dir := filepath.Join(t.TempDir(), "store")
	repo, err := filesystem.NewAuthRepository(dir, "personal")
	if err != nil {
		t.Fatal(err)
	}
	repo.Close()
	if err = os.WriteFile(filepath.Join(dir, strings.Repeat("a", 64)+".json"), []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if repo, err = filesystem.NewAuthRepository(dir, "personal"); err == nil {
		repo.Close()
		t.Fatal("unknown record version accepted")
	}
}
