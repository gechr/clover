package rust

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

const (
	// host is the web origin for Rust, used as the Describe label.
	host = "rust-lang.org"
	// indexURL is the public manifest index: one line per channel manifest ever
	// published, chronological (oldest first), in a single text response.
	indexURL = "https://static.rust-lang.org/manifests.txt"
	// releasePath is the rust-lang/rust release-notes directory a stable version
	// tag hangs off, for the Linker. Only stable releases are tagged there.
	releasePath = "https://github.com/rust-lang/rust/releases/tag/"
)

// manifestLine picks apart one index line, e.g.
// static.rust-lang.org/dist/2026-07-09/channel-rust-1.97.0.toml: the directory
// date the manifest was published under and the token naming it.
var manifestLine = regexp.MustCompile(
	`^static\.rust-lang\.org/dist/(\d{4}-\d{2}-\d{2})/channel-rust-(.+)\.toml$`,
)

// stableToken and betaToken admit the version-named manifest tokens that denote
// a release. Admission is by shape, not parseability, because the index also
// carries alias manifests that would collide once parsed: the minor alias 1.97
// normalizes to 1.97.0, and the moving beta aliases 1.75-beta and 1.75.0-beta
// both normalize to 1.75.0-beta. Only the full X.Y.Z form is a stable release,
// and only the numbered X.Y.Z-beta.N form is a beta snapshot.
var (
	stableToken = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	betaToken   = regexp.MustCompile(`^\d+\.\d+\.\d+-beta\.\d+$`)
)

// entry is one version-named manifest parsed from the index: its version token
// and the date of the directory it was published under.
type entry struct {
	version string
	date    time.Time
}

// Discover lists candidate Rust versions for the resource's channel, reading
// the manifest index in one fetch. The index holds the whole release history in
// a single response, so there is no pagination and nothing is ever left unread -
// --deep has no work to do here. A version re-published under a later directory
// (a fixed-up manifest) keeps its first date: that is when the release shipped,
// which is what cooldown measures.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("rust: invalid resource %T", r)
	}

	entries, err := p.fetch(ctx)
	if err != nil {
		return nil, err
	}

	token := stableToken
	if res.channel == channelBeta {
		token = betaToken
	}

	seen := make(map[string]bool, len(entries))
	candidates := make([]model.Candidate, 0, len(entries))
	for _, e := range entries {
		if !token.MatchString(e.version) || seen[e.version] {
			continue
		}
		seen[e.version] = true
		semver, err := version.Parse(e.version)
		if err != nil {
			continue // defensive: a shape-admitted token always parses
		}
		candidates = append(candidates, model.Candidate{
			Version:     e.version,
			Semver:      semver,
			Ref:         e.version,
			PublishedAt: e.date,
		})
	}
	return candidates, nil
}

// fetch downloads the manifest index and parses its version-named lines, in
// index (chronological) order. Channel-named lines (stable, beta, nightly) and
// anything else non-version-shaped fall out later at the token check.
func (p *Provider) fetch(ctx context.Context) ([]entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("rust: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rust: fetch manifest index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError("rust: list releases", resp)
	}

	var entries []entry
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		m := manifestLine.FindStringSubmatch(scanner.Text())
		if m == nil {
			continue
		}
		date, err := time.Parse(time.DateOnly, m[1])
		if err != nil {
			continue // defensive: a date-shaped directory that is not a real date
		}
		entries = append(entries, entry{version: m[2], date: date})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("rust: read manifest index: %w", err)
	}
	return entries, nil
}
