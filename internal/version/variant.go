package version

import (
	"strings"

	"github.com/gechr/x/set"
)

// variants are the recognized image variant suffixes - distro flavours and
// codenames that decorate a tag (nginx:1.27-alpine) and must be preserved, as
// opposed to a semver prerelease (2.0.0-rc.1) which go-version also parses after
// a dash. The set is curated; an unknown trailing segment is a prerelease.
var variants = set.New(
	"alpine",
	"slim",
	// Debian release codenames.
	"buster",
	"bullseye",
	"bookworm",
	"trixie",
	"sid",
	// Ubuntu release codenames.
	"bionic",
	"focal",
	"jammy",
	"noble",
)

// IsVariant reports whether a trailing dash-segment is a recognized image
// variant (a suffix to preserve) rather than a prerelease. It matches the first
// dash-delimited word with trailing version digits stripped, so alpine3.19 and
// slim-bookworm both register.
func IsVariant(segment string) bool {
	word, _, _ := strings.Cut(segment, "-")
	word = strings.TrimRight(word, "0123456789.")
	return variants.Contains(strings.ToLower(word))
}

// Qualifier returns the trailing dash-suffix of a tag - the prerelease or
// variant portion, recognized or not: "1.15.0-ent" -> "ent", "1.27-slim-bookworm"
// -> "slim-bookworm", "v3.5.0" -> "", "3.18.0" -> "". Build metadata (+...) is
// dropped. Unlike [SplitVariant], which only reports a curated set, this reports
// whatever decorates the version, so a marker can scope selection to its own
// suffix without that suffix needing to be on the list.
func Qualifier(tag string) string {
	_, rest, found := strings.Cut(strings.TrimPrefix(tag, "v"), "-")
	if !found {
		return ""
	}
	rest, _, _ = strings.Cut(rest, "+")
	return rest
}

// SplitVariant separates a recognized image variant suffix from tag, returning
// the base (tag without the variant) and the variant ("" when none). It splits
// on the first dash: "1.27-alpine" -> ("1.27", "alpine"), while a true
// prerelease is left intact: "2.0.0-rc.1" -> ("2.0.0-rc.1", ""). This lets a
// variant tag be ordered by its numeric core rather than as a (lower-sorting)
// prerelease.
func SplitVariant(tag string) (string, string) {
	base, rest, found := strings.Cut(tag, "-")
	if !found || !IsVariant(rest) {
		return tag, ""
	}
	return base, rest
}
