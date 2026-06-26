package docker

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/provider"
)

// Digest resolves the content digest a tag points at, delegating to the shared
// OCI client. A Hub reference resolves via the registry (not the web API), with
// credentials keyed under the Hub auth host - see [reference.manifestRepo].
func (p *Provider) Digest(ctx context.Context, r provider.Resource, tag string) (string, error) {
	ref, ok := r.(reference)
	if !ok {
		return "", fmt.Errorf("docker: invalid resource %T", r)
	}
	return p.client.Digest(ctx, ref.manifestRepo(), tag)
}
