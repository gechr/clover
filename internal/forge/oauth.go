package forge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
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

// NextLink returns the rel="next" URL from an RFC 8288 Link header, or "" when
// none. Forges that paginate with the standard Link header expose the
// authoritative "more pages" signal here, rather than guessing from a full page.
func NextLink(header http.Header) string {
	for part := range strings.SplitSeq(header.Get("Link"), ",") {
		ref, attrs, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok {
			continue
		}
		link := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(ref), "<"), ">")
		for attr := range strings.SplitSeq(attrs, ";") {
			if strings.TrimSpace(attr) == `rel="next"` {
				return link
			}
		}
	}
	return ""
}
