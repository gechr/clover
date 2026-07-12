package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/provider"
)

// Credentialed reports whether a credential applies to the resource's host,
// satisfying provider.CredentialChecker. It gates the default verification
// tier.
func (p *Provider) Credentialed(r provider.Resource) bool {
	res, ok := r.(resource)
	return ok && p.authenticated(res.host)
}

// DefaultBranch returns the repository's default branch, for tag-on-trunk
// verification when no explicit allowed-branch pattern is set.
func (p *Provider) DefaultBranch(ctx context.Context, r provider.Resource) (string, error) {
	res, ok := r.(resource)
	if !ok {
		return "", fmt.Errorf("github: invalid resource %T", r)
	}

	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	url := apiURL(res.host, fmt.Sprintf("repos/%s/%s", res.owner, res.name))
	if _, err := p.client().
		DoWithContext(ctx, url, forge.Bearer(p.credential(res.host)), &repo); err != nil {
		return "", fmt.Errorf("github: resolve default branch: %w", err)
	}
	return repo.DefaultBranch, nil
}

// Branches lists the repository's branches with their tip commits, for matching
// an allowed-branch pattern and the tip-equality fast path. It always paginates
// to exhaustion - a release branch (e.g. v1.12) can sort well past the first
// page - since a partial list could silently miss the allowed branch.
func (p *Provider) Branches(ctx context.Context, r provider.Resource) ([]provider.Branch, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("github: invalid resource %T", r)
	}
	rest := p.client()
	token := p.credential(res.host)

	type apiBranch struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	var branches []provider.Branch
	for page := 1; ; page++ {
		var batch []apiBranch
		url := apiURL(res.host, fmt.Sprintf(
			"repos/%s/%s/branches?per_page=%d&page=%d",
			res.owner,
			res.name,
			perPage,
			page,
		))
		if _, err := rest.DoWithContext(ctx, url, forge.Bearer(token), &batch); err != nil {
			return nil, fmt.Errorf("github: list branches: %w", err)
		}
		for _, b := range batch {
			branches = append(branches, provider.Branch{Name: b.Name, Tip: b.Commit.SHA})
		}
		if len(batch) < perPage { // a short page is the last
			break
		}
	}
	return branches, nil
}

// Reachable reports whether commit is an ancestor of (or equal to) branch's tip,
// via the compare API: a base that is "behind" or "identical" to the head
// contains it.
func (p *Provider) Reachable(
	ctx context.Context,
	r provider.Resource,
	branch, commit string,
) (bool, error) {
	res, ok := r.(resource)
	if !ok {
		return false, fmt.Errorf("github: invalid resource %T", r)
	}

	var cmp struct {
		Status string `json:"status"`
	}
	url := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/compare/%s...%s", res.owner, res.name, branch, commit),
	)
	if _, err := p.client().
		DoWithContext(ctx, url, forge.Bearer(p.credential(res.host)), &cmp); err != nil {
		// The compare endpoint 404s when the two refs share no common history -
		// e.g. a commit fabricated outside the repository - which is a definitive
		// "not reachable", not an API failure.
		var status *forge.StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("github: compare %s...%s: %w", branch, commit, err)
	}
	return cmp.Status == "behind" || cmp.Status == "identical", nil
}
