package forge

import (
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	xstrings "github.com/gechr/x/strings"
)

// KeySource selects what a forge marker discovers; SourceTags and
// SourceReleases are its two values. Every forge serves both.
const (
	KeySource      = "source"
	SourceTags     = "tags"
	SourceReleases = "releases"
)

// OwnerName parses the required repository key as owner/name, framing errors
// with the provider label.
func OwnerName(label string, d directive.Directive) (string, string, error) {
	repo, ok := d.Get(constant.DirectiveRepository)
	if !ok {
		return "", "", fmt.Errorf("%s: %q is required", label, constant.DirectiveRepository)
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || xstrings.AnyEmpty(owner, name) || strings.Contains(name, "/") {
		return "", "", fmt.Errorf(
			"%s: %q must be owner/name, got %q",
			label,
			constant.DirectiveRepository,
			repo,
		)
	}
	return owner, name, nil
}

// Host resolves the optional host key against a forge's default host,
// normalizing and validating an explicit value.
func Host(label string, d directive.Directive, defaultHost string) (string, error) {
	h, ok := d.Get(constant.DirectiveHost)
	if !ok {
		return defaultHost, nil
	}
	host, valid := NormalizeHost(h)
	if !valid {
		return "", fmt.Errorf(
			"%s: %q must be a valid host, got %q",
			label,
			constant.DirectiveHost,
			h,
		)
	}
	return host, nil
}

// Source resolves the optional source key, defaulting to tags and rejecting
// anything but tags or releases.
func Source(label string, d directive.Directive) (string, error) {
	s, ok := d.Get(KeySource)
	if !ok {
		return SourceTags, nil
	}
	if s != SourceTags && s != SourceReleases {
		return "", fmt.Errorf(
			"%s: %q must be %s or %s, got %q",
			label,
			KeySource,
			SourceTags,
			SourceReleases,
			s,
		)
	}
	return s, nil
}

// RequireReleasesForAsset rejects asset= on a non-releases source: asset=
// filters on release asset filenames, which only releases publish, so a tag
// candidate has none and the filter would always fail later.
func RequireReleasesForAsset(label string, d directive.Directive, source string) error {
	if _, ok := d.Get(constant.RuleAsset); ok && source != SourceReleases {
		return fmt.Errorf(
			"%s: %q requires %q to be %q",
			label,
			constant.RuleAsset,
			KeySource,
			SourceReleases,
		)
	}
	return nil
}
