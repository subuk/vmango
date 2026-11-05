package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// SigningMethodEd25519 uses the fully specified JOSE identifier from RFC 9864.
// The JWT library supplies the Ed25519 signing and verification implementation.
var SigningMethodEd25519 jwt.SigningMethod = signingMethodEd25519{jwt.SigningMethodEdDSA}

type signingMethodEd25519 struct{ *jwt.SigningMethodEd25519 }

func (signingMethodEd25519) Alg() string { return "Ed25519" }

func init() {
	jwt.RegisterSigningMethod(SigningMethodEd25519.Alg(), func() jwt.SigningMethod {
		return SigningMethodEd25519
	})
}

// ParseSigningKey accepts one unencrypted PKCS#8 PEM Ed25519 private key.
func ParseSigningKey(value string) (ed25519.PrivateKey, error) {
	value = strings.TrimSpace(value)
	block, rest := pem.Decode([]byte(value))
	if block == nil || !strings.HasPrefix(value, "-----BEGIN PRIVATE KEY-----") || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("signing key must contain one unencrypted PKCS#8 PEM Ed25519 private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid PKCS#8 signing key")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("signing key must be an Ed25519 private key")
	}
	return key, nil
}

func ValidateKey(value string) error {
	_, err := ParseSigningKey(value)
	return err
}

// ValidateSigningKeys compares key material, independent of PEM formatting.
func ValidateSigningKeys(apiKey, consoleKey string) error {
	a, err := ParseSigningKey(apiKey)
	if err != nil {
		return err
	}
	b, err := ParseSigningKey(consoleKey)
	if err != nil {
		return err
	}
	if a.Equal(b) {
		return errors.New("JWT and console signing keys must be distinct")
	}
	return nil
}
