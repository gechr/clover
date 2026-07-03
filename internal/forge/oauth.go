package forge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// PKCE returns an RFC 7636 verifier (32 random bytes, 43 base64url chars) and its
// S256 challenge (the base64url SHA-256 of the verifier), as required by a public
// OAuth client.
func PKCE() (string, string, error) {
	b := make([]byte, 32) //nolint:mnd // self-explanatory
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// RandomState returns an unguessable OAuth state value (16 random bytes) binding
// an authorization request to its callback.
func RandomState() (string, error) {
	b := make([]byte, 16) //nolint:mnd // self-explanatory
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
