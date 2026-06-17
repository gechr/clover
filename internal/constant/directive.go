package constant

// Keyword is the directive sigil cusp scans for inside a comment. Everything
// after it on the line is the directive the user wrote.
const Keyword = "cusp:"

// Equal separates a directive key from its value. It is a rune because the
// parser scans character by character; use string(Equal) where a string is
// needed (e.g. rendering a directive in format mode).
const Equal = '='
