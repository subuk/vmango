package auth_test

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"subuk/vmango/auth"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestEd25519TokenSignature(t *testing.T) {
	s := setup(t)
	raw, err := s.WebToken(principal("admin"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := auth.ParseSigningKey(signingKey)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(raw, ".")
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var h map[string]any
	if err := json.Unmarshal(header, &h); err != nil || h["alg"] != "Ed25519" {
		t.Fatal("unexpected JWT algorithm", string(header), err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !ed25519.Verify(key.Public().(ed25519.PublicKey), []byte(parts[0]+"."+parts[1]), sig) {
		t.Fatal("JWT signature cannot be verified with public key", err)
	}
	claims, err := s.Authenticate(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Both old shared secrets and public-key-as-HMAC-secret attacks must fail.
	for _, secret := range [][]byte{[]byte(strings.Repeat("abcdef01", 8)), []byte(key.Public().(ed25519.PublicKey))} {
		forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Authenticate(forged); err == nil {
			t.Fatal("accepted HMAC token")
		}
	}
}

func TestParseSigningKey(t *testing.T) {
	key, err := auth.ParseSigningKey("\n" + signingKey + "\n")
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(signingKey))
	block.Headers = map[string]string{"Comment": "unsupported"}
	for _, bad := range []string{
		"", strings.Repeat("abcdef01", 8), "junk\n" + signingKey,
		signingKey + "junk", signingKey + signingKey,
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
		string(pem.EncodeToMemory(block)),
	} {
		if _, err := auth.ParseSigningKey(bad); err == nil {
			t.Fatal("accepted invalid private key")
		}
	}
}
