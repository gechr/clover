package docker

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/attest"
	"github.com/gechr/clover/internal/provider"
)

// VerifyAttestation verifies a digest's Sigstore attestation against its signer
// policy, delegating registry access and cryptographic verification.
func (p *Provider) VerifyAttestation(
	ctx context.Context,
	r provider.Resource,
	digest string,
	policy provider.AttestationPolicy,
) (bool, error) {
	ref, ok := r.(reference)
	if !ok {
		return false, fmt.Errorf("docker: invalid resource %T", r)
	}
	bundles, err := p.client.ReferrerArtifacts(
		ctx,
		ref.manifestRepo(),
		digest,
		attest.BundleMediaTypePrefix,
	)
	if err != nil {
		return false, err
	}
	return p.attestor.Verify(ctx, bundles, digest, policy.Identity, policy.Issuer)
}
