package ignore

// Option configures a [Matcher].
type Option func(*Matcher)

// WithDisabled turns the matcher into a no-op that ignores nothing, for
// --no-ignore. VCS directories are pruned by the walker, not here, so they stay
// excluded regardless.
func WithDisabled() Option {
	return func(m *Matcher) { m.disabled = true }
}

// WithFiles sets the ignore file names read in each directory, lowest priority
// first (default: .gitignore). This is the seam for a future .cloverignore.
func WithFiles(names ...string) Option {
	return func(m *Matcher) { m.files = names }
}
