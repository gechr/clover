package attest

import "github.com/sigstore/sigstore-go/pkg/root"

// Option configures a [Verifier].
type Option func(*Verifier)

// WithTrustedMaterial supplies trusted Sigstore material. A nil value keeps the
// lazily fetched public-good trust root.
func WithTrustedMaterial(material root.TrustedMaterial) Option {
	return func(v *Verifier) {
		if material != nil {
			v.material = material
		}
	}
}
