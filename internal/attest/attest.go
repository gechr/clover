package attest

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/gechr/clover/internal/pattern"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	// BundleMediaTypePrefix matches modern Sigstore bundle artifact media types,
	// including version parameters and versioned subtype spellings.
	BundleMediaTypePrefix = "application/vnd.dev.sigstore.bundle"

	// DefaultIssuer is GitHub Actions' OIDC token issuer.
	DefaultIssuer = "https://token.actions.githubusercontent.com"
)

// Verifier verifies Sigstore bundles against a trusted root. The public-good
// root is fetched lazily so markers that do not opt in cause no TUF traffic.
type Verifier struct {
	material root.TrustedMaterial
	once     sync.Once
	verifier *verify.Verifier
	err      error
}

// New returns a Sigstore bundle verifier.
func New(opts ...Option) *Verifier {
	v := &Verifier{}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify reports whether any bundle cryptographically verifies for digest and
// matches the certificate identity policy. Malformed and nonmatching bundles
// are skipped; trust-root setup failures are returned as infrastructure errors.
func (v *Verifier) Verify(
	ctx context.Context,
	bundles [][]byte,
	digest, identity, issuer string,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(bundles) == 0 {
		return false, nil
	}
	if err := v.initialize(); err != nil {
		return false, err
	}

	algorithm, encoded, ok := strings.Cut(digest, ":")
	if !ok || algorithm == "" || encoded == "" {
		return false, fmt.Errorf("attest: invalid digest %q", digest)
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return false, fmt.Errorf("attest: invalid digest %q: %w", digest, err)
	}
	certID, err := certificateIdentity(identity, issuer)
	if err != nil {
		return false, err
	}
	policy := verify.NewPolicy(
		verify.WithArtifactDigest(algorithm, decoded),
		verify.WithCertificateIdentity(certID),
	)

	for _, contents := range bundles {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		entity := &bundle.Bundle{}
		if err := entity.UnmarshalJSON(contents); err != nil {
			continue
		}
		if _, err := v.verifier.Verify(entity, policy); err == nil {
			return true, nil
		}
	}
	return false, nil
}

func (v *Verifier) initialize() error {
	v.once.Do(func() {
		material := v.material
		if material == nil {
			material, v.err = root.FetchTrustedRootWithOptions(
				tuf.DefaultOptions().WithCacheValidity(1),
			)
			if v.err != nil {
				v.err = fmt.Errorf("attest: fetch Sigstore trusted root: %w", v.err)
				return
			}
		}
		v.verifier, v.err = verify.NewVerifier(
			material,
			verify.WithSignedCertificateTimestamps(1),
			verify.WithTransparencyLog(1),
			verify.WithObserverTimestamps(1),
		)
		if v.err != nil {
			v.err = fmt.Errorf("attest: configure Sigstore verifier: %w", v.err)
		}
	})
	return v.err
}

func certificateIdentity(identity, issuer string) (verify.CertificateIdentity, error) {
	pat, err := pattern.Compile(identity)
	if err != nil {
		return verify.CertificateIdentity{}, err
	}
	// Anchor both dialects to a full-string match. sigstore matches the SAN
	// regex as an unanchored substring, so an unanchored identity (a raw
	// /regex/) would accept an attacker SAN that merely contains it - to require
	// a substring the user writes the wildcards explicitly.
	sanRegex := "^(?:" + pat.Regexp().String() + ")$"
	if issuer == "" {
		issuer = DefaultIssuer
	}
	certID, err := verify.NewShortCertificateIdentity(issuer, "", "", sanRegex)
	if err != nil {
		return verify.CertificateIdentity{}, fmt.Errorf("attest: certificate identity: %w", err)
	}
	return certID, nil
}
