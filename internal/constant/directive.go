package constant

// DirectiveEqual separates a directive key from its value. It is a rune because
// the parser scans character by character; use string(DirectiveEqual) where a
// string is needed (e.g. rendering a directive in format mode).
const DirectiveEqual = '='

// DirectiveKeyword is the sigil cusp scans for inside a comment. Everything
// after it on the line is the directive the user wrote.
const DirectiveKeyword = "cusp:"
