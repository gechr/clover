package constant

// Constraint keyword values. The constraint key accepts either one of these
// bump-ceiling keywords or a go-version range expression; these name the
// keyword dialect. Referenced by the version package's constraint parser today
// and, as they land, the format canonicaliser and the CLI --constraint enum.
const (
	Major = "major"
	Minor = "minor"
	Patch = "patch"
)
