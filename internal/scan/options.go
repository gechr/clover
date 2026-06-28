package scan

// IgnoreFunc reports whether the walk should skip path. isDir distinguishes a
// directory (skipped wholesale) from a file. It is the seam a gitignore matcher
// plugs into via [WithIgnore].
type IgnoreFunc func(path string, isDir bool) bool

// config is the resolved set of [Scan] options.
type config struct {
	workers          int
	maxSize          int64
	ignore           IgnoreFunc
	requireDirective bool
}

// Option configures [Scan].
type Option func(*config)

// WithWorkers sets the number of files scanned concurrently (default: NumCPU).
func WithWorkers(n int) Option { return func(c *config) { c.workers = n } }

// WithMaxSize sets the largest file scan will read.
func WithMaxSize(n int64) Option { return func(c *config) { c.maxSize = n } }

// WithIgnore supplies the predicate that skips ignored files and directories -
// the seam a gitignore matcher plugs into. It is consulted in addition to the
// always-skipped VCS directories.
func WithIgnore(fn IgnoreFunc) Option {
	return func(c *config) { c.ignore = fn }
}

// WithRequireDirective controls whether a file must already carry a clover:
// directive to be returned. The default (true) is what run, lint, and format
// want: only annotated files are of interest, and the keyword prefilter skips
// the rest cheaply. annotate sets it false to inspect every text file,
// proposing directives for lines that carry none - so a keyword-less file is
// returned with an empty Found rather than discarded.
func WithRequireDirective(on bool) Option {
	return func(c *config) { c.requireDirective = on }
}
