package version

import "strings"

// variants are the recognized image variant suffixes - distro flavours and
// codenames that decorate a tag (nginx:1.27-alpine) and must be preserved, as
// opposed to a semver prerelease (2.0.0-rc.1) which go-version also parses after
// a dash. The set is curated; an unknown trailing segment is a prerelease.
var variants = map[string]bool{
	"alpine": true,
	"slim":   true,
	// Debian release codenames.
	"buster":   true,
	"bullseye": true,
	"bookworm": true,
	"trixie":   true,
	"sid":      true,
	// Ubuntu release codenames.
	"bionic": true,
	"focal":  true,
	"jammy":  true,
	"noble":  true,
}

// IsVariant reports whether a trailing dash-segment is a recognized image
// variant (a suffix to preserve) rather than a prerelease. It matches the first
// dash-delimited word with trailing version digits stripped, so alpine3.19 and
// slim-bookworm both register.
func IsVariant(segment string) bool {
	word, _, _ := strings.Cut(segment, "-")
	word = strings.TrimRight(word, "0123456789.")
	return variants[strings.ToLower(word)]
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
