// Package auth owns credential profiles, session rotation and token lifecycle.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AccessLifetime      = 10 * time.Minute
	SessionLifetime     = 7 * 24 * time.Hour
	PersonalLifetime    = 90 * 24 * time.Hour
	MaxPersonalLifetime = 365 * 24 * time.Hour
	MaxSessions         = 32
	MaxTokens           = 64
	MaxRotations        = 4096
	Issuer              = "vmango"
	Audience            = "vmango-api"
)

var (
	ErrInvalid      = errors.New("invalid credential")
	ErrInvalidInput = errors.New("invalid credential request")
	ErrExpired      = errors.New("expired credential")
	ErrForbidden    = errors.New("credential is not permitted")
	ErrLimit        = errors.New("credential limit reached")
	ErrNotFound     = errors.New("credential record not found")
	validID         = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func ValidID(id string) bool { return validID.MatchString(id) }

// Principal keeps stable ownership separate from mutable presentation claims.
type Principal struct {
	Method        string `json:"method"`
	LocalID       string `json:"local_id,omitempty"`
	Issuer        string `json:"issuer,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`
	Name          string `json:"name,omitempty"`
}

func (p Principal) ID() string {
	var components []string
	switch p.Method {
	case "local":
		if p.LocalID == "" {
			return ""
		}
		components = []string{"local", p.LocalID}
	case "oidc":
		if p.Issuer == "" || p.Subject == "" {
			return ""
		}
		components = []string{"oidc", p.Issuer, p.Subject}
	default:
		return ""
	}
	b, _ := json.Marshal(components)
	return base64.RawURLEncoding.EncodeToString(b)
}

type Claims struct {
	jwt.RegisteredClaims
	Kind      string    `json:"token_kind"`
	Purpose   string    `json:"purpose"`
	Principal Principal `json:"principal"`
}

// Record is versioned mutable authentication data; it never contains a bearer secret.
type Record struct {
	Version    int        `json:"version"`
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Owner      Principal  `json:"owner"`
	Name       string     `json:"name,omitempty"`
	Digest     string     `json:"digest"`
	Previous   []string   `json:"previous,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Revoked    bool       `json:"revoked"`
}

// Repository serializes reads/updates with durable revocation. Update must read
// fresh state and persist only when the callback returns changed=true.
type Repository interface {
	Create(Record, int) error
	Update(string, func(*Record) (bool, error)) error
	List() ([]Record, error)
	Cleanup(time.Time) error
}

type TokenMetadata struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Owner      string     `json:"owner"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Status     string     `json:"status"`
}

type Service struct {
	key              []byte
	sessions, tokens Repository
	eligible         func(Principal) bool
	now              func() time.Time
	streamsMu        sync.Mutex
	streams          map[string]map[*streamHandle]struct{}
}

type streamHandle struct{ close func() }

func New(key string, sessions, tokens Repository, eligible func(Principal) bool) (*Service, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	if sessions == nil || tokens == nil || eligible == nil {
		return nil, errors.New("authentication repositories and eligibility policy are required")
	}
	return &Service{key: []byte(key), sessions: sessions, tokens: tokens, eligible: eligible, now: time.Now, streams: make(map[string]map[*streamHandle]struct{})}, nil
}

func ValidateKey(key string) error {
	if len(key) < 32 || len(key) > 4096 || strings.Contains(strings.ToLower(key), "changeme") || strings.Contains(strings.ToLower(key), "replace") {
		return errors.New("jwt_signing_key must contain at least 32 random bytes; generate a persistent key with openssl rand -hex 32")
	}
	distinct := map[rune]bool{}
	for _, c := range key {
		distinct[c] = true
	}
	if len(distinct) < 8 {
		return errors.New("jwt_signing_key has insufficient diversity; generate it with openssl rand -hex 32")
	}
	return nil
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func same(a, b string) bool  { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (s *Service) sign(p Principal, kind, id string, lifetime time.Duration) (string, error) {
	now := s.now().UTC().Truncate(time.Second)
	c := Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Audience: jwt.ClaimStrings{Audience}, Subject: p.ID(), ID: id, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(lifetime))}, Kind: kind, Purpose: "access", Principal: p}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
}

func (s *Service) WebToken(p Principal) (string, error) {
	if p.ID() == "" || !s.eligible(p) {
		return "", ErrForbidden
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	return s.sign(p, "web", id, AccessLifetime)
}

func (s *Service) Authenticate(raw string) (*Claims, error) {
	if len(raw) > 16384 {
		return nil, ErrInvalid
	}
	c := &Claims{}
	_, err := jwt.ParseWithClaims(raw, c, func(*jwt.Token) (any, error) { return s.key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(Issuer), jwt.WithAudience(Audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(s.now))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) && !errors.Is(err, jwt.ErrTokenSignatureInvalid) && !errors.Is(err, jwt.ErrTokenMalformed) {
			return nil, ErrExpired
		}
		return nil, ErrInvalid
	}
	if c.Purpose != "access" || c.Subject == "" || c.Subject != c.Principal.ID() || c.IssuedAt == nil || c.ExpiresAt == nil || !ValidID(c.ID) || !c.ExpiresAt.After(c.IssuedAt.Time) {
		return nil, ErrInvalid
	}
	switch c.Kind {
	case "web":
		if c.ExpiresAt.Sub(c.IssuedAt.Time) > AccessLifetime {
			return nil, ErrInvalid
		}
	case "personal":
		if c.ExpiresAt.Sub(c.IssuedAt.Time) > MaxPersonalLifetime || !s.eligible(c.Principal) {
			return nil, ErrInvalid
		}
		if err := s.checkPersonal(raw, c); err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalid
	}
	return c, nil
}

func (s *Service) checkPersonal(raw string, c *Claims) error {
	updateUsage := false
	err := s.tokens.Update(c.ID, func(r *Record) (bool, error) {
		if r.Kind != "personal" || r.Revoked || r.Owner.ID() != c.Subject || !same(r.Digest, digest(raw)) || !s.now().Before(r.ExpiresAt) || r.ExpiresAt.Unix() != c.ExpiresAt.Unix() {
			return false, ErrInvalid
		}
		updateUsage = r.LastUsedAt == nil || s.now().Sub(*r.LastUsedAt) >= time.Minute
		return false, nil
	})
	if errors.Is(err, ErrNotFound) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if !updateUsage {
		return nil
	}
	// Usage telemetry is best effort and cannot alter authorization or resurrect a record.
	_ = s.tokens.Update(c.ID, func(r *Record) (bool, error) {
		if r.Revoked || !same(r.Digest, digest(raw)) {
			return false, nil
		}
		now := s.now().UTC()
		if r.LastUsedAt != nil && now.Sub(*r.LastUsedAt) < time.Minute {
			return false, nil
		}
		r.LastUsedAt = &now
		return true, nil
	})
	return nil
}

func (s *Service) Login(p Principal) (access, refresh string, expires time.Time, err error) {
	if p.ID() == "" || !s.eligible(p) {
		err = ErrForbidden
		return
	}
	id, e := randomID()
	if e != nil {
		err = e
		return
	}
	secret, e := randomID()
	if e != nil {
		err = e
		return
	}
	access, err = s.WebToken(p)
	if err != nil {
		return
	}
	now := s.now().UTC()
	expires = now.Add(SessionLifetime)
	r := Record{Version: 1, ID: id, Kind: "refresh", Owner: p, Digest: digest(secret), CreatedAt: now, ExpiresAt: expires}
	err = s.sessions.Create(r, MaxSessions)
	if err == nil {
		refresh = id + "." + secret
	} else {
		access = ""
		expires = time.Time{}
	}
	return
}

func refreshParts(raw string) (string, string, error) {
	p := strings.Split(raw, ".")
	if len(p) != 2 || !ValidID(p[0]) || !ValidID(p[1]) {
		return "", "", ErrInvalid
	}
	return p[0], p[1], nil
}

func (s *Service) Refresh(raw string) (access, refresh string, expires time.Time, err error) {
	id, secret, e := refreshParts(raw)
	if e != nil {
		err = e
		return
	}
	next, e := randomID()
	if e != nil {
		err = e
		return
	}
	reused := false
	err = s.sessions.Update(id, func(r *Record) (bool, error) {
		if r.Kind != "refresh" || r.Revoked || !s.now().Before(r.ExpiresAt) {
			return false, ErrInvalid
		}
		hash := digest(secret)
		if !same(r.Digest, hash) {
			for _, prev := range r.Previous {
				if same(prev, hash) {
					r.Revoked = true
					reused = true
					return true, nil
				}
			}
			return false, ErrInvalid
		}
		if !s.eligible(r.Owner) || len(r.Previous) >= MaxRotations {
			r.Revoked = true
			reused = true
			return true, nil
		}
		var signErr error
		access, signErr = s.WebToken(r.Owner)
		if signErr != nil {
			return false, signErr
		}
		r.Previous = append(r.Previous, r.Digest)
		r.Digest = digest(next)
		expires = r.ExpiresAt
		return true, nil
	})
	if reused || errors.Is(err, ErrNotFound) {
		err = ErrInvalid
	}
	if err == nil {
		refresh = id + "." + next
	} else {
		access = ""
		expires = time.Time{}
	}
	return
}

func (s *Service) Logout(raw string) error {
	id, secret, err := refreshParts(raw)
	if err != nil {
		return nil
	}
	err = s.sessions.Update(id, func(r *Record) (bool, error) {
		if r.Kind != "refresh" || r.Revoked {
			return false, nil
		}
		hash := digest(secret)
		known := same(hash, r.Digest)
		for _, prev := range r.Previous {
			known = known || same(prev, hash)
		}
		if !known {
			return false, nil
		}
		r.Revoked = true
		return true, nil
	})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func interactive(c *Claims) bool { return c != nil && c.Kind == "web" }
func (s *Service) CreateToken(c *Claims, name string, days int) (TokenMetadata, string, error) {
	if !interactive(c) {
		return TokenMetadata{}, "", ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || days < 1 || days > 365 {
		return TokenMetadata{}, "", ErrInvalidInput
	}
	if !s.eligible(c.Principal) {
		return TokenMetadata{}, "", ErrForbidden
	}
	id, err := randomID()
	if err != nil {
		return TokenMetadata{}, "", err
	}
	raw, err := s.sign(c.Principal, "personal", id, time.Duration(days)*24*time.Hour)
	if err != nil {
		return TokenMetadata{}, "", err
	}
	now := s.now().UTC().Truncate(time.Second)
	r := Record{Version: 1, ID: id, Kind: "personal", Owner: c.Principal, Name: name, Digest: digest(raw), CreatedAt: now, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour)}
	if err = s.tokens.Create(r, MaxTokens); err != nil {
		return TokenMetadata{}, "", err
	}
	return s.metadata(r), raw, nil
}
func (s *Service) metadata(r Record) TokenMetadata {
	status := "active"
	if !s.now().Before(r.ExpiresAt) {
		status = "expired"
	}
	if r.Revoked {
		status = "revoked"
	}
	return TokenMetadata{r.ID, r.Name, r.Owner.ID(), r.CreatedAt, r.ExpiresAt, r.LastUsedAt, status}
}
func (s *Service) ListTokens(c *Claims) ([]TokenMetadata, error) {
	if !interactive(c) {
		return nil, ErrForbidden
	}
	rs, err := s.tokens.List()
	if err != nil {
		return nil, err
	}
	out := []TokenMetadata{}
	for _, r := range rs {
		if r.Owner.ID() == c.Subject {
			out = append(out, s.metadata(r))
		}
	}
	return out, nil
}
func (s *Service) RevokeToken(c *Claims, id string) error {
	if !interactive(c) {
		return ErrForbidden
	}
	if !ValidID(id) {
		return ErrInvalid
	}
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	err := s.tokens.Update(id, func(r *Record) (bool, error) {
		if r.Owner.ID() != c.Subject {
			return false, ErrNotFound
		}
		if r.Revoked {
			return false, nil
		}
		r.Revoked = true
		return true, nil
	})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for h := range s.streams[id] {
		h.close()
	}
	delete(s.streams, id)
	return nil
}

// RegisterStream rechecks the disk record under the same lock as API revocation.
// Only transient connection handles are retained; no authorization records are cached.
func (s *Service) RegisterStream(raw string, c *Claims, closeStream func()) (func(), error) {
	if c.Kind != "personal" {
		return func() {}, nil
	}
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if _, err := s.Authenticate(raw); err != nil {
		return nil, err
	}
	h := &streamHandle{closeStream}
	if s.streams[c.ID] == nil {
		s.streams[c.ID] = map[*streamHandle]struct{}{}
	}
	s.streams[c.ID][h] = struct{}{}
	return func() {
		s.streamsMu.Lock()
		defer s.streamsMu.Unlock()
		delete(s.streams[c.ID], h)
		if len(s.streams[c.ID]) == 0 {
			delete(s.streams, c.ID)
		}
	}, nil
}

func (s *Service) Cleanup() error {
	if err := s.sessions.Cleanup(s.now()); err != nil {
		return fmt.Errorf("session cleanup: %w", err)
	}
	if err := s.tokens.Cleanup(s.now()); err != nil {
		return fmt.Errorf("token cleanup: %w", err)
	}
	return nil
}
