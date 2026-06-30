package scan

// IgnoreFunc reports whether the walk should skip path. isDir distinguishes a
// directory (skipped wholesale) from a file. It is the seam a gitignore matcher
// plugs into via [WithIgnore].
type IgnoreFunc func(path string, isDir bool) bool

// ProgressFunc reports the running count of files examined so far. It is called
// once per file from the scan worker pool, so an implementation must be
// goroutine-safe; it is the seam a live progress display plugs into.
type ProgressFunc func(scanned int)

// config is the resolved set of [Scan] options.
type config struct {
	workers          int
	maxSize          int64
	ignore           IgnoreFunc
	progress         ProgressFunc
	requireDirective bool
}

// Option configures [Scan].
type Option func(*config)

// WithWorkers sets the number of files scanned concurrently (default: NumCPU).
func WithWorkers(n int) Option { return func(c *config) { c.workers = n } }

// WithMaxSize sets the largest file scan will read.
func WithMaxSize(n int64) Option { return func(c *config) { c.maxSize = n } }

// WithProgress supplies a callback invoked once per file as the walk proceeds,
// carrying the running count of files examined. It is the seam the CLI's live
// scan-progress line plugs into; the callback must be goroutine-safe.
func WithProgress(fn ProgressFunc) Option {
	return func(c *config) { c.progress = fn }
}

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
