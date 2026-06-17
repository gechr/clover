package version

import (
	"fmt"

	goversion "github.com/hashicorp/go-version"
)

// Version is the parsed, comparable form of a version string. cusp uses
// hashicorp/go-version directly as its semver value type: it is the de-facto
// engine (Terraform's own) and the design commits to it, so wrapping it would
// be indirection paid for a swap nobody has planned.
type Version = goversion.Version

// Parse is cusp's canonical entry point for turning a version string into a
// comparable [Version]. It tolerates a leading v and one-to-three components
// (go-version pads missing components with zeros). The wrapped error names the
// offending input so callers need not restate it.
func Parse(s string) (*Version, error) {
	v, err := goversion.NewVersion(s)
	if err != nil {
		return nil, fmt.Errorf("parse version %q: %w", s, err)
	}
	return v, nil
}
