package model

import (
	"time"

	"github.com/gechr/clover/internal/version"
)

// NewCandidate returns the candidate for a raw published version: Version and
// Ref carry it verbatim and Semver holds its parse, nil when it is not
// semver-shaped. Optional metadata (commit, publication time, assets) is set by
// the caller on the result.
func NewCandidate(raw string) Candidate {
	semver, _ := version.Parse(raw)
	return Candidate{Version: raw, Semver: semver, Ref: raw}
}

// NewVariantCandidate returns the candidate for a tag that may carry a variant
// suffix (e.g. 1.27-alpine): the suffix is stripped before parsing so the tag
// orders by its numeric core rather than as a prerelease, while a true
// prerelease (2.0.0-rc.1) is kept.
func NewVariantCandidate(raw string) Candidate {
	base, _ := version.SplitVariant(raw)
	semver, _ := version.Parse(base)
	return Candidate{Version: raw, Semver: semver, Ref: raw}
}

// Candidate is one version a provider discovered, enriched with whatever
// metadata the provider's API returned for free at discovery. clover carries this
// whole record forward - never collapsing it to a bare version string - so a
// later stage (rendering a side value, verifying a commit) already has what it
// needs in hand and no stage has to reach backwards. Providers fill the fields
// they can; the rest stay zero.
//
// Typed fields cover the metadata clover expects to use; the open Meta bag holds
// provider-specific extras without growing the struct.
type Candidate struct {
	// Version is the raw tag or release name as published, e.g. "v1.27.0" or
	// "1.27-alpine". It is what include/exclude match against and what style
	// preservation reads.
	Version string

	// Semver is Version parsed for comparison, or nil when it is not
	// semver-shaped (calver, a commit pin) and so cannot be ordered.
	Semver *version.Version

	// Prerelease marks a version the upstream declares a prerelease out of band
	// of its tag - e.g. a GitHub release flagged pre-release on a clean tag.
	// Selection excludes it like a semver prerelease unless prereleases are
	// allowed. Providers with no such signal leave it false.
	Prerelease bool

	// PublishedAt is when the version was released, used by cooldown. Zero when
	// the provider's listing does not carry a timestamp.
	PublishedAt time.Time

	// Commit is the commit SHA the version points at, when the provider exposes
	// one (e.g. a GitHub tag or release target).
	Commit string

	// Ref is the upstream reference the version came from, e.g. the tag name a
	// release was cut from.
	Ref string

	// Digest is the content digest the version resolves to (e.g. an OCI image's
	// sha256 manifest digest), for secure-pin rewriting. Empty until resolved.
	Digest string

	// Assets are the downloadable files the version publishes (e.g. a GitHub
	// release's assets), the source for auto-sourcing a follower's sha256.
	Assets []Asset

	// Meta holds provider-specific values that do not warrant a typed field.
	Meta map[string]string
}

// Asset is one downloadable file a version publishes. Digest is the provider's
// own content digest when it supplies one (e.g. sha256:...), letting clover
// source a checksum without a download. URL is the public browser download URL;
// APIURL is the forge API endpoint serving the same content, which honors a
// credential where the browser URL does not, keeping a private repository's
// assets downloadable.
type Asset struct {
	Name   string
	Digest string
	URL    string
	APIURL string
}
