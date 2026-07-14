package crates

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	xstrings "github.com/gechr/x/strings"
)

const (
	// host is the web origin for crates.io, used as the Describe label root and
	// the Linker URL host.
	host = "crates.io"
	// origin is the scheme-qualified origin the API's relative dl_path resolves
	// against.
	origin = "https://crates.io"
	// apiPath is the registry API root: apiPath/<crate>/versions returns the
	// crate's whole version history in one response.
	apiPath = "https://crates.io/api/v1/crates/"
	// cratePath is the crate-page root the Linker builds from.
	cratePath = "https://crates.io/crates/"
)

// listing is the subset of a versions response the provider reads: the list of
// published versions.
type listing struct {
	Versions []release `json:"versions"`
}

// release is the subset of a version record the provider reads: the publish
// time feeds cooldown, the yanked flag drops withdrawn versions, and the
// .crate file's sha256 checksum and download path surface as an asset.
type release struct {
	Num       string            `json:"num"`
	CreatedAt dates.ReleaseTime `json:"created_at"`
	Yanked    bool              `json:"yanked"`
	Checksum  string            `json:"checksum"`
	DLPath    string            `json:"dl_path"`
}

// Discover lists a crate's candidate versions, reading the registry API in one
// fetch. The API returns the whole version history in a single response, so
// there is no pagination and nothing is ever left unread - --deep has no work
// to do here. Candidates are sorted naturally for a deterministic listing.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("crates: invalid resource %T", r)
	}

	releases, err := p.fetch(ctx, res.name)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		if rel.Yanked {
			continue // withdrawn from the registry: not installable
		}
		semver, err := version.Parse(rel.Num)
		if err != nil {
			// Cargo enforces semver on publish, but drop an unparseable record
			// rather than surface it.
			continue
		}
		candidates = append(candidates, candidate(res.name, rel, semver))
	}
	slices.SortFunc(candidates, func(a, b model.Candidate) int {
		// The ref breaks a tie between two raw spellings of one canonical
		// version, keeping the listing deterministic.
		return cmp.Or(
			xstrings.CompareNatural(a.Version, b.Version),
			xstrings.CompareNatural(a.Ref, b.Ref),
		)
	})
	return candidates, nil
}

// fetch downloads and decodes a crate's versions listing.
func (p *Provider) fetch(ctx context.Context, name string) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiPath+name+"/versions", nil)
	if err != nil {
		return nil, fmt.Errorf("crates: build request: %w", err)
	}
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crates: fetch versions listing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError("crates: list "+name+" versions", resp)
	}

	var l listing
	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		return nil, fmt.Errorf("crates: decode versions listing: %w", err)
	}
	return l.Versions, nil
}

// candidate builds a model.Candidate from a crate name, a version record, and
// its parse. Version is the parsed semver's canonical form with the raw
// registry form kept on Ref, PublishedAt is the publish time for cooldown, and
// the .crate file surfaces as an asset with its sha256 checksum, letting a
// follower source a checksum without a download.
func candidate(name string, rel release, semver *version.Version) model.Candidate {
	digest := rel.Checksum
	if digest != "" {
		digest = constant.DigestSha256 + digest
	}
	var url string
	if rel.DLPath != "" {
		url = origin + rel.DLPath
	}
	return model.Candidate{
		Version:     semver.String(),
		Semver:      semver,
		Ref:         rel.Num,
		PublishedAt: rel.CreatedAt.Time,
		Assets: []model.Asset{
			{Name: name + "-" + rel.Num + ".crate", Digest: digest, URL: url},
		},
	}
}
