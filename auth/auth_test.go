package auth_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/auth"
	"subuk/vmango/filesystem"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const signingKey = "e702acd09d76676cd386642a315038470691f711b3821e1552bfffd37cb15b983"

type testService struct {
	*auth.Service
	sessions, tokens *filesystem.AuthRepository
	dir              string
}

func setup(t *testing.T) testService {
	t.Helper()
	dir := t.TempDir()
	sessions, err := filesystem.NewAuthRepository(filepath.Join(dir, "sessions"), "refresh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sessions.Close() })
	tokens, err := filesystem.NewAuthRepository(filepath.Join(dir, "tokens"), "personal")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tokens.Close() })
	s, err := auth.New(signingKey, sessions, tokens, func(p auth.Principal) bool { return p.ID() != "" })
	if err != nil {
		t.Fatal(err)
	}
	return testService{s, sessions, tokens, dir}
}
func principal(id string) auth.Principal {
	return auth.Principal{Method: "local", LocalID: id, Email: "same@example.com"}
}
func interactive(t *testing.T, s *auth.Service, id string) *auth.Claims {
	t.Helper()
	raw, err := s.WebToken(principal(id))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Authenticate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRefreshRotationReuseAndForgedSecret(t *testing.T) {
	s := setup(t)
	_, raw, absolute, err := s.Login(principal("admin"))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Split(raw, ".")[0]
	if _, _, _, err = s.Refresh(id + "." + strings.Repeat("0", 64)); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("forged credential accepted", err)
	}
	_, next, expiry, err := s.Refresh(raw)
	if err != nil || next == raw || !expiry.Equal(absolute) {
		t.Fatal("rotation did not retain absolute expiry", err)
	}
	// This also defines lost-response recovery: the old cookie requires login.
	if _, _, _, err = s.Refresh(raw); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("rotated credential reused", err)
	}
	if _, _, _, err = s.Refresh(next); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("reused family was not revoked", err)
	}
}

func TestRefreshConcurrentAndLogout(t *testing.T) {
	s := setup(t)
	access, raw, _, err := s.Login(principal("admin"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var successes atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, err := s.Refresh(raw); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("rotation was not atomic", successes.Load())
	}
	if _, err := s.Authenticate(access); err != nil {
		t.Fatal("refresh revocation changed stateless web JWT", err)
	}
	_, raw, _, err = s.Login(principal("admin"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(raw); err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(raw); err != nil {
		t.Fatal("logout not idempotent", err)
	}
	if _, _, _, err = s.Refresh(raw); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("logout did not revoke refresh", err)
	}
}

func TestPersonalDiskChecksRevocationAndIsolation(t *testing.T) {
	s := setup(t)
	owner := interactive(t, s.Service, "admin")
	other := interactive(t, s.Service, "other")
	m, raw, err := s.CreateToken(owner, "CI provisioning", 90)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(raw); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "tokens", m.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), raw) {
		t.Fatal("bearer secret persisted")
	}
	if err = s.RevokeToken(other, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(raw); err != nil {
		t.Fatal("another owner revoked credential", err)
	}
	if list, err := s.ListTokens(other); err != nil || len(list) != 0 {
		t.Fatal("another owner's token listed", err)
	}
	c, err := s.Authenticate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.CreateToken(c, "derived", 365); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("personal token minted another token")
	}
	if _, err = s.ListTokens(c); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("personal token managed tokens")
	}
	closed := false
	unregister, err := s.RegisterStream(raw, c, func() { closed = true })
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	if err = s.RevokeToken(owner, m.ID); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("active personal console was not closed")
	}
	if _, err = s.Authenticate(raw); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("revocation not immediately enforced", err)
	}
	if _, err = s.RegisterStream(raw, c, func() {}); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("handshake escaped revocation", err)
	}
	list, err := s.ListTokens(owner)
	if err != nil || len(list) != 1 || list[0].Status != "revoked" {
		t.Fatal("telemetry resurrected token", list, err)
	}
}

func TestMissingCorruptAndRestartedStores(t *testing.T) {
	s := setup(t)
	owner := interactive(t, s.Service, "admin")
	m, raw, err := s.CreateToken(owner, "CI", 90)
	if err != nil {
		t.Fatal(err)
	}
	_, refresh, _, err := s.Login(principal("admin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = filesystem.NewAuthRepository(filepath.Join(s.dir, "tokens"), "personal"); err == nil {
		t.Fatal("second process/store owner accepted")
	}
	s.tokens.Close()
	s.sessions.Close()
	tokens, err := filesystem.NewAuthRepository(filepath.Join(s.dir, "tokens"), "personal")
	if err != nil {
		t.Fatal(err)
	}
	defer tokens.Close()
	sessions, err := filesystem.NewAuthRepository(filepath.Join(s.dir, "sessions"), "refresh")
	if err != nil {
		t.Fatal(err)
	}
	defer sessions.Close()
	restarted, err := auth.New(signingKey, sessions, tokens, func(auth.Principal) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Authenticate(raw); err != nil {
		t.Fatal("restart lost PAT", err)
	}
	if _, _, _, err = restarted.Refresh(refresh); err != nil {
		t.Fatal("restart lost session", err)
	}
	path := filepath.Join(s.dir, "tokens", m.ID+".json")
	if err = os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Authenticate(raw); err == nil || errors.Is(err, auth.ErrInvalid) {
		t.Fatal("corruption must be a service error", err)
	}
	web, _ := restarted.WebToken(principal("admin"))
	if _, err = restarted.Authenticate(web); err != nil {
		t.Fatal("web JWT read broken store", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Authenticate(raw); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("missing record accepted", err)
	}
}

func TestJWTProfilesAndStableOwners(t *testing.T) {
	s := setup(t)
	now := time.Now().Truncate(time.Second)
	p := principal("admin")
	valid := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: auth.Issuer, Audience: jwt.ClaimStrings{auth.Audience}, Subject: p.ID(), ID: strings.Repeat("a", 64), IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(auth.AccessLifetime))}, Kind: "web", Purpose: "access", Principal: p}
	for _, tc := range []struct {
		name      string
		change    func(*auth.Claims)
		algorithm *jwt.SigningMethodHMAC
		valid     bool
	}{
		{"valid", func(*auth.Claims) {}, jwt.SigningMethodHS256, true},
		{"missing kind", func(c *auth.Claims) { c.Kind = "" }, jwt.SigningMethodHS256, false},
		{"unknown kind", func(c *auth.Claims) { c.Kind = "other" }, jwt.SigningMethodHS256, false},
		{"wrong purpose", func(c *auth.Claims) { c.Purpose = "console" }, jwt.SigningMethodHS256, false},
		{"wrong audience", func(c *auth.Claims) { c.Audience = jwt.ClaimStrings{"console"} }, jwt.SigningMethodHS256, false},
		{"wrong issuer", func(c *auth.Claims) { c.Issuer = "other" }, jwt.SigningMethodHS256, false},
		{"missing expiry", func(c *auth.Claims) { c.ExpiresAt = nil }, jwt.SigningMethodHS256, false},
		{"missing issued at", func(c *auth.Claims) { c.IssuedAt = nil }, jwt.SigningMethodHS256, false},
		{"long web lifetime", func(c *auth.Claims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(time.Hour)) }, jwt.SigningMethodHS256, false},
		{"unregistered personal", func(c *auth.Claims) { c.Kind = "personal" }, jwt.SigningMethodHS256, false},
		{"wrong algorithm", func(*auth.Claims) {}, jwt.SigningMethodHS384, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid
			tc.change(&c)
			raw, err := jwt.NewWithClaims(tc.algorithm, &c).SignedString([]byte(signingKey))
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Authenticate(raw)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
	owners := []auth.Principal{p, {Method: "oidc", Issuer: "https://a", Subject: "1", Email: p.Email}, {Method: "oidc", Issuer: "https://a", Subject: "2", Email: p.Email}, {Method: "oidc", Issuer: "https://b", Subject: "1", Email: p.Email}}
	seen := map[string]bool{}
	for _, p := range owners {
		if seen[p.ID()] {
			t.Fatal("identities merged")
		}
		seen[p.ID()] = true
	}
	q := owners[1]
	q.Email = "changed@example.com"
	if q.ID() != owners[1].ID() {
		t.Fatal("email owns tokens")
	}
	m, raw, err := s.CreateToken(interactive(t, s.Service, "admin"), "CI", 365)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(m)
	if strings.Contains(string(data), raw) {
		t.Fatal("metadata contains bearer")
	}
	rotated, err := auth.New(strings.Replace(signingKey, "e702", "abcd", 1), s.sessions, s.tokens, func(auth.Principal) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rotated.Authenticate(raw); err == nil {
		t.Fatal("key rotation accepted old PAT")
	}
}
