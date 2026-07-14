package gitea

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/provider"
)

// Credentialed reports whether a credential applies to the resource's host,
// satisfying provider.CredentialChecker. It gates the default verification
// tier. An expired login still counts when it carries a refresh token, since
// the next request renews it.
func (p *Provider) Credentialed(r provider.Resource) bool {
	res, ok := r.(resource)
	if !ok {
		return false
	}
	if pat := p.staticCredential(); pat != "" && res.host == p.patHost() {
		return true
	}
	if p.store == nil {
		return false
	}
	c, ok := p.storedCreds(res.host)
	return ok && (!c.expired() || c.RefreshToken != "")
}

// DefaultBranch returns the repository's default branch, for tag-on-trunk
// verification when no explicit allowed-branch pattern is set.
func (p *Provider) DefaultBranch(ctx context.Context, r provider.Resource) (string, error) {
	res, ok := r.(resource)
	if !ok {
		return "", fmt.Errorf("gitea: invalid resource %T", r)
	}
	url := apiURL(res.host, fmt.Sprintf("repos/%s/%s", res.owner, res.name))
	return forge.DefaultBranch(
		ctx, p.rest,
		constant.ProviderGitea, url, p.authorization(ctx, res.host),
	)
}

// Branches lists the repository's branches with their tip commits, for matching
// an allowed-branch pattern and the tip-equality fast path.
func (p *Provider) Branches(ctx context.Context, r provider.Resource) ([]provider.Branch, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("gitea: invalid resource %T", r)
	}
	start := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/branches?limit=%d", res.owner, res.name, perPage),
	)
	return forge.Branches(
		ctx, p.rest,
		constant.ProviderGitea, start, p.authorization(ctx, res.host),
	)
}

// Reachable reports whether commit is an ancestor of (or equal to) branch's
// tip, via the compare API.
func (p *Provider) Reachable(
	ctx context.Context,
	r provider.Resource,
	branch, commit string,
) (bool, error) {
	res, ok := r.(resource)
	if !ok {
		return false, fmt.Errorf("gitea: invalid resource %T", r)
	}
	url := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/compare/%s...%s", res.owner, res.name, branch, commit),
	)
	return forge.Reachable(
		ctx, p.rest,
		constant.ProviderGitea, url, p.authorization(ctx, res.host), branch, commit,
	)
}
