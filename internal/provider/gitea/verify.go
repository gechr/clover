package gitea

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/provider"
	xhttp "github.com/gechr/x/http"
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

	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	url := apiURL(res.host, fmt.Sprintf("repos/%s/%s", res.owner, res.name))
	if _, err := p.rest.
		DoWithContext(ctx, url, p.authorization(ctx, res.host), &repo); err != nil {
		return "", fmt.Errorf("gitea: resolve default branch: %w", err)
	}
	return repo.DefaultBranch, nil
}

// Branches lists the repository's branches with their tip commits, for matching
// an allowed-branch pattern and the tip-equality fast path. It always paginates
// to exhaustion - a release branch can sort well past the first page - following
// the Link header, since a partial list could silently miss the allowed branch.
func (p *Provider) Branches(ctx context.Context, r provider.Resource) ([]provider.Branch, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("gitea: invalid resource %T", r)
	}
	authorization := p.authorization(ctx, res.host)

	type apiBranch struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	start := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/branches?limit=%d", res.owner, res.name, perPage),
	)
	var branches []provider.Branch
	for url := start; url != ""; {
		var batch []apiBranch
		header, err := p.rest.DoWithContext(ctx, url, authorization, &batch)
		if err != nil {
			return nil, fmt.Errorf("gitea: list branches: %w", err)
		}
		for _, b := range batch {
			branches = append(branches, provider.Branch{Name: b.Name, Tip: b.Commit.ID})
		}
		next := xhttp.NextLink(header)
		// Never follow a next page to a different origin: the credential must not
		// leak off the host the lookup started on.
		if next != "" && !forge.SameOrigin(start, next) {
			next = ""
		}
		url = next
	}
	return branches, nil
}

// Reachable reports whether commit is an ancestor of (or equal to) branch's
// tip, via the compare API: a head whose history holds no commits beyond the
// base is contained by it.
func (p *Provider) Reachable(
	ctx context.Context,
	r provider.Resource,
	branch, commit string,
) (bool, error) {
	res, ok := r.(resource)
	if !ok {
		return false, fmt.Errorf("gitea: invalid resource %T", r)
	}

	var cmp struct {
		TotalCommits int `json:"total_commits"`
	}
	url := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/compare/%s...%s", res.owner, res.name, branch, commit),
	)
	if _, err := p.rest.
		DoWithContext(ctx, url, p.authorization(ctx, res.host), &cmp); err != nil {
		// A 404 means a ref the repository does not contain - a definitive "not
		// reachable", not an API failure.
		var status *forge.StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("gitea: compare %s...%s: %w", branch, commit, err)
	}
	return cmp.TotalCommits == 0, nil
}
