package github

import (
	"context"
	"fmt"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/provider"
)

// Credentialed reports whether a credential applies to the resource's host,
// satisfying provider.CredentialChecker. It gates the default verification
// tier, so an anonymous rate limit is never spent on verification the user did
// not ask for.
func (p *Provider) Credentialed(r provider.Resource) bool {
	res, ok := r.(resource)
	return ok && p.credential(res.host) != ""
}

// DefaultBranch returns the repository's default branch, for tag-on-trunk
// verification when no explicit allowed-branch pattern is set.
func (p *Provider) DefaultBranch(ctx context.Context, r provider.Resource) (string, error) {
	res, ok := r.(resource)
	if !ok {
		return "", fmt.Errorf("github: invalid resource %T", r)
	}
	url := apiURL(res.host, fmt.Sprintf("repos/%s/%s", res.owner, res.name))
	return forge.DefaultBranch(
		ctx, p.client(),
		constant.ProviderGithub, url, forge.Bearer(p.credential(res.host)),
	)
}

// Branches lists the repository's branches with their tip commits, for matching
// an allowed-branch pattern and the tip-equality fast path.
func (p *Provider) Branches(ctx context.Context, r provider.Resource) ([]provider.Branch, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("github: invalid resource %T", r)
	}
	start := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/branches?per_page=%d", res.owner, res.name, perPage),
	)
	return forge.Branches(
		ctx, p.client(),
		constant.ProviderGithub, start, forge.Bearer(p.credential(res.host)),
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
		return false, fmt.Errorf("github: invalid resource %T", r)
	}
	url := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/compare/%s...%s", res.owner, res.name, branch, commit),
	)
	return forge.Reachable(
		ctx, p.client(),
		constant.ProviderGithub, url, forge.Bearer(p.credential(res.host)), branch, commit,
	)
}
