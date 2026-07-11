package token

// Option configures a [Store].
type Option func(*Store)

// WithDir overrides the file-fallback directory, for tests.
func WithDir(dir string) Option {
	return func(s *Store) { s.dir = dir }
}
