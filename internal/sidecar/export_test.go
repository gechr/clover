package sidecar

// Exposed for black-box tests of the package's resolution internals.
var (
	ParsePath     = parsePath
	ResolveJQLine = resolveJQLine
)
