package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

const (
	// host is the web origin for Node.js, used as the Describe label and the
	// Linker URL root.
	host = "nodejs.org"
	// indexURL is the public release index: every Node.js release, newest-first,
	// in one JSON response.
	indexURL = "https://nodejs.org/dist/index.json"
)

// release is the subset of a Node.js index entry the provider reads. lts is
// false for a current release or the line's codename string (e.g. "Iron") for an
// LTS release, so it is decoded raw and interpreted by isLTS.
type release struct {
	Version string          `json:"version"`
	Date    string          `json:"date"`
	LTS     json.RawMessage `json:"lts"`
}

// isLTS reports whether the release belongs to an LTS line: the index gives a
// codename string for LTS releases and the literal false for current ones.
func (r release) isLTS() bool {
	s := strings.TrimSpace(string(r.LTS))
	return s != "" && s != "false" && s != "null"
}

// Discover lists candidate Node.js versions, reading the release index in one
// fetch. The index holds the whole release history newest-first in a single
// response, so there is no pagination and nothing is ever left unread - --deep
// has no work to do here.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("node: invalid resource %T", r)
	}

	releases, err := p.fetch(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		if rel.Version == "" || !res.matches(rel) {
			continue
		}
		candidates = append(candidates, candidate(rel))
	}
	return candidates, nil
}

// fetch downloads and decodes the release index.
func (p *Provider) fetch(ctx context.Context) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("node: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("node: fetch release index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf(
			"node: list releases: %s (%s)",
			strings.TrimSpace(string(msg)),
			resp.Status,
		)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("node: decode release index: %w", err)
	}
	return releases, nil
}

// candidate builds a model.Candidate from a release, parsing its v-prefixed
// version for comparison and carrying the publication date the index supplied for
// free. A version that is not semver-shaped yields a nil Semver and is skipped by
// selection.
func candidate(rel release) model.Candidate {
	semver, _ := version.Parse(rel.Version)
	published, _ := time.Parse(time.DateOnly, rel.Date)
	return model.Candidate{
		Version:     rel.Version,
		Semver:      semver,
		PublishedAt: published,
		Ref:         rel.Version,
	}
}

// matches reports whether a release belongs to the requested scope: an LTS
// resource keeps only the LTS lines; the default keeps every release.
func (res resource) matches(rel release) bool {
	if res.lts {
		return rel.isLTS()
	}
	return true
}
