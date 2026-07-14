// This file holds the verification helpers shared by the forges whose REST
// APIs expose the same shapes (GitHub and Gitea): a repository document
// carrying default_branch, a Link-header-paginated branch list, and a compare
// endpoint whose total_commits counts the head's commits beyond the base. Each
// provider supplies its own URL builder and authorization; GitLab's API
// differs (X-Next-Page pagination, a merge_base endpoint), so it keeps its own
// implementation.

package forge

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/provider"
	xhttp "github.com/gechr/x/http"
)

// DefaultBranch reads the repository document at url and returns its default
// branch, for tag-on-trunk verification when no explicit allowed-branch pattern
// is set. prefix names the calling provider in errors.
func DefaultBranch(
	ctx context.Context,
	rest RESTClient,
	prefix, url, authorization string,
) (string, error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if _, err := rest.DoWithContext(ctx, url, authorization, &repo); err != nil {
		return "", fmt.Errorf("%s: resolve default branch: %w", prefix, err)
	}
	return repo.DefaultBranch, nil
}

// Branches lists a repository's branches with their tip commits, for matching
// an allowed-branch pattern and the tip-equality fast path. It always paginates
// to exhaustion - a release branch can sort well past the first page - following
// the Link header, since a partial list could silently miss the allowed branch.
// The tip is read from whichever field the forge spells it as: sha (GitHub) or
// id (Gitea).
func Branches(
	ctx context.Context,
	rest RESTClient,
	prefix, start, authorization string,
) ([]provider.Branch, error) {
	type apiBranch struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
			ID  string `json:"id"`
		} `json:"commit"`
	}
	var branches []provider.Branch
	for url := start; url != ""; {
		var batch []apiBranch
		header, err := rest.DoWithContext(ctx, url, authorization, &batch)
		if err != nil {
			return nil, fmt.Errorf("%s: list branches: %w", prefix, err)
		}
		for _, b := range batch {
			branches = append(branches, provider.Branch{
				Name: b.Name,
				Tip:  cmp.Or(b.Commit.SHA, b.Commit.ID),
			})
		}
		next := xhttp.NextLink(header)
		// Never follow a next page to a different origin: the credential must not
		// leak off the host the lookup started on.
		if next != "" && !SameOrigin(start, next) {
			next = ""
		}
		url = next
	}
	return branches, nil
}

// Reachable reports whether commit is an ancestor of (or equal to) branch's
// tip, via the compare document at url (base=branch, head=commit): a head whose
// history holds no commits beyond the base is contained by it.
func Reachable(
	ctx context.Context,
	rest RESTClient,
	prefix, url, authorization, branch, commit string,
) (bool, error) {
	var compared struct {
		TotalCommits int `json:"total_commits"`
	}
	if _, err := rest.DoWithContext(ctx, url, authorization, &compared); err != nil {
		// A 404 means a ref the repository does not contain - a definitive "not
		// reachable", not an API failure.
		var status *StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("%s: compare %s...%s: %w", prefix, branch, commit, err)
	}
	return compared.TotalCommits == 0, nil
}
