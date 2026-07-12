package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"

	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/provider"
)

// Credentialed reports whether a credential applies to the resource's host,
// satisfying provider.CredentialChecker. It gates the default verification
// tier.
func (p *Provider) Credentialed(r provider.Resource) bool {
	res, ok := r.(resource)
	return ok && p.credential(res.host) != ""
}

// DefaultBranch returns the project's default branch, for tag-on-trunk
// verification when no explicit allowed-branch pattern is set.
func (p *Provider) DefaultBranch(ctx context.Context, r provider.Resource) (string, error) {
	res, ok := r.(resource)
	if !ok {
		return "", fmt.Errorf("gitlab: invalid resource %T", r)
	}

	var project struct {
		DefaultBranch string `json:"default_branch"`
	}
	url := apiURL(res.host, "projects/"+res.projectID())
	if _, err := p.rest.
		DoWithContext(ctx, url, forge.Bearer(p.credential(res.host)), &project); err != nil {
		return "", fmt.Errorf("gitlab: resolve default branch: %w", err)
	}
	return project.DefaultBranch, nil
}

// Branches lists the project's branches with their tip commits, for matching an
// allowed-branch pattern and the tip-equality fast path. It always paginates to
// exhaustion - a release branch can sort well past the first page - following
// the X-Next-Page header, since a partial list could silently miss the allowed
// branch.
func (p *Provider) Branches(ctx context.Context, r provider.Resource) ([]provider.Branch, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("gitlab: invalid resource %T", r)
	}
	token := p.credential(res.host)

	type apiBranch struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	var branches []provider.Branch
	for page := 1; ; page++ {
		var batch []apiBranch
		url := apiURL(res.host, fmt.Sprintf(
			"projects/%s/repository/branches?per_page=%d&page=%d",
			res.projectID(),
			perPage,
			page,
		))
		header, err := p.rest.DoWithContext(ctx, url, forge.Bearer(token), &batch)
		if err != nil {
			return nil, fmt.Errorf("gitlab: list branches: %w", err)
		}
		for _, b := range batch {
			branches = append(branches, provider.Branch{Name: b.Name, Tip: b.Commit.ID})
		}
		if header.Get("X-Next-Page") == "" {
			return branches, nil
		}
	}
}

// Reachable reports whether commit is an ancestor of (or equal to) branch's
// tip: the merge base of the two refs is the commit itself exactly when the
// branch contains it.
func (p *Provider) Reachable(
	ctx context.Context,
	r provider.Resource,
	branch, commit string,
) (bool, error) {
	res, ok := r.(resource)
	if !ok {
		return false, fmt.Errorf("gitlab: invalid resource %T", r)
	}

	refs := neturl.Values{}
	refs.Add("refs[]", branch)
	refs.Add("refs[]", commit)
	var base struct {
		ID string `json:"id"`
	}
	url := apiURL(res.host, "projects/"+res.projectID()+"/repository/merge_base?"+refs.Encode())
	if _, err := p.rest.
		DoWithContext(ctx, url, forge.Bearer(p.credential(res.host)), &base); err != nil {
		// A 404 means the refs share no common ancestor - a definitive "not
		// reachable", not an API failure.
		var status *forge.StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("gitlab: merge base of %s and %s: %w", branch, commit, err)
	}
	return base.ID == commit, nil
}
