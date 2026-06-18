// Package field defines the structured-log field names clover logs with, so a
// key is spelled identically everywhere and a misspelling is a compile error.
package field

const (
	Changed  = "changed"
	Comments = "comments"
	Elapsed  = "elapsed"
	Errored  = "errored"
	Failed   = "failed"
	Files    = "files"
	From     = "from"
	Hint     = "hint"
	Location = "location"
	Path     = "path"
	Provider = "provider"
	Reason   = "reason"
	Resource = "resource"
	Scanned  = "scanned"
	Skipped  = "skipped"
	Tags     = "tags"
	To       = "to"
	Version  = "version"
)
