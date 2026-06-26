// Package ignore matches paths against the ignore files that apply to them, for
// use as scan's ignore predicate. It is modelled on ripgrep's ignore handling
// but owned in-repo - no vendored walker or matcher: a configurable set of
// per-directory ignore files (default .gitignore; a .cloverignore can be added
// via WithFiles) sharing gitignore syntax. For a path it consults every ignore
// file from the repository root (via vcs) down to the path's directory, last
// match winning, so nested ignores and negation behave as git does. Patterns
// compile to RE2; parsed files are cached per directory.
package ignore
