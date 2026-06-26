// Package output defines the detail level of a clover report. It is a leaf
// package - depending on nothing else in the tree - so both the config layer
// (which stores it as a setting) and the report layer (which renders at it) can
// share one type without coupling to each other.
package output

// Mode is the detail level of a report. Its string values double as the CLI's
// --output enum and the config's output setting.
type Mode string

const (
	// Text is the concise default: only changes and problems, plus a summary.
	Text Mode = "text"
	// Wide additionally reports every marker that was already up to date or
	// valid, so the output accounts for all of them.
	Wide Mode = "wide"
	// GitHub emits GitHub Actions annotations (::error/::warning file=,line=) so
	// changes and problems surface inline on a pull request.
	GitHub Mode = "github"
)
