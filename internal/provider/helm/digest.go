package helm

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/provider"
)

// Digest resolves the content digest a chart tag points at, for digest-pinned
// rewriting. It is available only for oci:// charts; a classic repository
// already exposes the chart-tarball digest as an asset on each candidate.
func (p *Provider) Digest(ctx context.Context, r provider.Resource, tag string) (string, error) {
	ref, ok := r.(reference)
	if !ok {
		return "", fmt.Errorf("helm: invalid resource %T", r)
	}
	if !ref.isOCI {
		return "", fmt.Errorf(
			"helm: digest is only available for oci:// charts, not %s",
			ref.baseURL,
		)
	}
	return p.client.Digest(ctx, ref.repo, tag)
}
