package pipeline

import (
	"runtime"
	"time"

	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/progress"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/tag"
)

// defaultScanLabel is the transient scan-progress message when a command sets
// none of its own.
const defaultScanLabel = "Scanning files"

type settings struct {
	configs          *config.Resolver
	cooldown         *time.Duration
	current          string
	deep             *bool
	downgrade        *bool
	filter           tag.Filter
	force            *bool
	ignoreFiles      []string
	infer            bool
	maxSize          int64
	noIgnore         bool
	now              time.Time
	prerelease       *bool
	providerFilter   provider.Filter
	reporter         progress.Reporter
	requireDirective bool
	scanLabel        string
	to               string
	truncationSink   func(provider.Truncation)
	verify           *bool
	workers          int
}

// Option configures [Run].
type Option func(*settings)

// WithConfig sets the per-root config resolver. Each scanned file's repository
// root supplies its own paths.exclude, required-version gate, and selection
// toggle defaults (verify/prerelease/downgrade/deep); a CLI override still wins
// over every root. Without it the scan applies no project config.
func WithConfig(r *config.Resolver) Option {
	return func(s *settings) { s.configs = r }
}

// WithDeep overrides the per-root run.deep default for every marker: a deep
// lookup follows pagination to exhaustion instead of reading only the first
// (newest) page - more accurate, at the cost of more requests that may be slow or
// hit rate limits. A non-nil value forces it on or off run-wide; nil leaves each
// root's run.deep (and a verify-implied deep) in force. The default is shallow.
func WithDeep(deep *bool) Option { return func(s *settings) { s.deep = deep } }

// WithDowngrade overrides the per-directive downgrade rule for every
// marker: a non-nil allow forces downgrades on or off run-wide, while nil leaves
// each directive's own setting in force.
func WithDowngrade(allow *bool) Option {
	return func(s *settings) { s.downgrade = allow }
}

// WithForce overrides the per-root run.force default for every marker. When in
// force, a followed digest (sha256 or commit) is re-pinned even if the version
// it follows is unchanged, so a re-published artifact's new digest is adopted.
// nil leaves each root's run.force in force; the default holds an unchanged
// version's digest, so a pin never moves on its own.
func WithForce(force *bool) Option { return func(s *settings) { s.force = force } }

// WithIgnoreFiles sets the ignore-file names honoured during the walk (default:
// .gitignore).
func WithIgnoreFiles(names ...string) Option {
	return func(s *settings) { s.ignoreFiles = names }
}

// WithMaxSize sets the largest file the scan will read.
func WithMaxSize(n int64) Option { return func(s *settings) { s.maxSize = n } }

// WithNoIgnore disables ignore-file pruning (.gitignore) so otherwise-ignored
// files are scanned. VCS directories stay excluded.
func WithNoIgnore(on bool) Option {
	return func(s *settings) { s.noIgnore = on }
}

// WithNow injects the reference time cooldown measures against, keeping a run
// deterministic. Unset, the current time is used.
func WithNow(t time.Time) Option { return func(s *settings) { s.now = t } }

// WithPrerelease overrides the per-directive prerelease rule for every marker: a
// non-nil allow forces prereleases on or off run-wide, while nil leaves each
// directive's own setting in force.
func WithPrerelease(allow *bool) Option {
	return func(s *settings) { s.prerelease = allow }
}

// WithProviderFilter restricts the run to markers whose resolved provider the
// filter selects: an --enable list runs only those providers, a --disable list
// runs all but those. The manual provider always runs. The zero filter matches
// every marker.
func WithProviderFilter(f provider.Filter) Option {
	return func(s *settings) { s.providerFilter = f }
}

// WithReporter sets the progress reporter that observes markers as they resolve.
// The default discards everything; the CLI supplies a live display.
func WithReporter(r progress.Reporter) Option {
	return func(s *settings) { s.reporter = r }
}

// WithCooldown sets the CLI cooldown override: non-nil replaces every
// directive's own cooldown (zero disables cooldowns outright), nil leaves the
// per-directive rule and the config default in force.
func WithCooldown(d *time.Duration) Option {
	return func(s *settings) { s.cooldown = d }
}

// WithInfer enables synthetic markers for lines auto-detection recognizes but
// no written directive governs, so run updates them without any annotation.
// Pair it with WithRequireDirective(false) so files carrying no directives are
// scanned at all.
func WithInfer(on bool) Option {
	return func(s *settings) { s.infer = on }
}

// WithRequireDirective sets whether the scan keeps only files that already carry
// a clover: directive (the default, for run/lint/format) or returns every text
// file (for annotate, which proposes directives where none exist).
func WithRequireDirective(on bool) Option {
	return func(s *settings) { s.requireDirective = on }
}

// WithScanLabel sets the message shown on the transient scan-progress line - the
// live "Scanning ..." spinner that precedes resolution. Each command supplies its
// own phrasing; the default is generic.
func WithScanLabel(label string) Option {
	return func(s *settings) { s.scanLabel = label }
}

// WithTagFilter restricts the run to markers the filter matches. The zero filter
// matches every marker; a non-empty one drops markers whose tags do not satisfy
// it, including untagged markers.
func WithTagFilter(filter tag.Filter) Option {
	return func(s *settings) { s.filter = filter }
}

// WithTo pins every version-selected marker to the one version named,
// bypassing each directive's selection rules - the targeted-pin mode behind
// `run --to`. Markers that do not select a version (manual anchors, track=
// refs, followers) resolve as usual. Empty leaves normal selection in force.
func WithTo(v string) Option { return func(s *settings) { s.to = v } }

// WithTruncationSink sets a callback invoked with a truncated resource (its
// label and upstream page) when a shallow lookup stopped with more results
// available, so the caller can suggest a deep lookup. It may be called
// concurrently.
func WithTruncationSink(sink func(provider.Truncation)) Option {
	return func(s *settings) { s.truncationSink = sink }
}

// WithVerify overrides the per-directive verify rule for every marker: a non-nil
// value forces the deep tag-on-branch check on or off run-wide, while nil leaves
// each directive's own verify/verify-branch setting in force.
func WithVerify(on *bool) Option { return func(s *settings) { s.verify = on } }

// WithVersion sets the running clover version the per-root required-version gate
// checks each repository against. Unset (or an unparseable dev build), the gate
// is inert.
func WithVersion(current string) Option {
	return func(s *settings) { s.current = current }
}

// WithWorkers sets how many files the scan walk and markers resolve concurrently
// (library default: NumCPU; the CLI passes its -P flag, default 10).
func WithWorkers(n int) Option { return func(s *settings) { s.workers = n } }

// newSettings applies opts over the defaults, clamping the worker count and
// defaulting the clock so cooldown has a reference time.
func newSettings(opts ...Option) settings {
	set := settings{
		workers:          runtime.NumCPU(),
		reporter:         progress.Nop{},
		requireDirective: true,
		scanLabel:        defaultScanLabel,
	}
	for _, opt := range opts {
		opt(&set)
	}
	if set.workers < 1 {
		set.workers = 1
	}
	if set.now.IsZero() {
		set.now = time.Now()
	}
	if set.reporter == nil {
		set.reporter = progress.Nop{}
	}
	return set
}
