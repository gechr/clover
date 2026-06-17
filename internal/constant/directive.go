package constant

// DirectiveEqual separates a directive key from its value. It is a rune because
// the parser scans character by character; use string(DirectiveEqual) where a
// string is needed (e.g. rendering a directive in format mode).
const DirectiveEqual = '='

// DirectiveKeyword is the sigil cusp scans for inside a comment. Everything
// after it on the line is the directive the user wrote.
const DirectiveKeyword = "cusp:"

// Directive targeting and control keys: who resolves the marker and how it
// relates to others.
const (
	DirectiveFrom     = "from"     // follow the producer with this id
	DirectiveID       = "id"       // publish this marker's result under this id
	DirectiveProvider = "provider" // upstream source; omitted ⇒ follow
	DirectiveSelect   = "select"   // follow the old or new value
	DirectiveSkip     = "skip"     // disable this marker
	DirectiveValue    = "value"    // what a follower projects
)
