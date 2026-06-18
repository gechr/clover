package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	cversion "github.com/gechr/clive/version"
	"github.com/gechr/clover/internal/checksum"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/exec"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/ignore"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/progress"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/follow"
	"github.com/gechr/clover/internal/registry"
	"github.com/gechr/clover/internal/rule"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/tag"
	"github.com/gechr/clover/internal/vcs"
	"github.com/gechr/clover/internal/version"
)

// ErrNoCandidate reports that no discovered version satisfied the marker's rule
// (constraint, include/exclude, prerelease). The edge checks for it to suggest a
// deep lookup, since a shallow lookup may have paged past a matching version.
var ErrNoCandidate = errors.New("no candidate satisfies the rule")

// Result is the outcome of resolving one marker: the version it found in the
// file, the value it resolved to, and the rewritten target line. Exactly one of
// a clean resolution, Skipped, or Err holds. A skipped or errored marker leaves
// Resolved empty and NewLine equal to the original line.
type Result struct {
	Marker   Marker
	Current  string // the version token currently on the target line
	Resolved string // the value the marker resolved to upstream, or via follow
	NewLine  string // the target line after rendering, == original when unchanged
	Changed  bool   // whether rendering altered the target line
	Skipped  bool   // the marker's dependency failed, was missing, or cycled
	Reason   string // why the marker was skipped
	Err      error  // why resolution failed
}

// FileResult groups a scanned file's original lines with the results of every
// marker it carries, in line order. Lines is the content as read; Rewritten
// applies the changed results to produce the new content.
type FileResult struct {
	Path    string
	Lines   []string
	Results []Result
}

// Rewritten returns the file's lines with every changed result spliced onto its
// target line. The original slice is left untouched.
func (f FileResult) Rewritten() []string {
	lines := make([]string, len(f.Lines))
	copy(lines, f.Lines)
	for _, r := range f.Results {
		if r.Changed && r.Marker.Target >= 0 && r.Marker.Target < len(lines) {
			lines[r.Marker.Target] = r.NewLine
		}
	}
	return lines
}

type config struct {
	allowDowngrade *bool
	deep           bool
	exclude        []string
	filter         tag.Filter
	ignoreFiles    []string
	maxSize        int64
	now            time.Time
	prerelease     *bool
	reporter       progress.Reporter
	truncationSink func(resource string)
	workers        int
}

// Option configures [Run].
type Option func(*config)

// WithReporter sets the progress reporter that observes markers as they resolve.
// The default discards everything; the CLI supplies a live display.
func WithReporter(r progress.Reporter) Option {
	return func(c *config) { c.reporter = r }
}

// WithExclude sets the doublestar globs whose matching paths are skipped during
// the scan, in addition to the ignore files.
func WithExclude(globs []string) Option {
	return func(c *config) { c.exclude = globs }
}

// WithTagFilter restricts the run to markers the filter matches. The zero filter
// matches every marker; a non-empty one drops markers whose tags do not satisfy
// it, including untagged markers.
func WithTagFilter(filter tag.Filter) Option {
	return func(c *config) { c.filter = filter }
}

// WithAllowDowngrade overrides the per-directive allow-downgrade rule for every
// marker: a non-nil allow forces downgrades on or off run-wide, while nil leaves
// each directive's own setting in force.
func WithAllowDowngrade(allow *bool) Option {
	return func(c *config) { c.allowDowngrade = allow }
}

// WithPrerelease overrides the per-directive prerelease rule for every marker: a
// non-nil allow forces prereleases on or off run-wide, while nil leaves each
// directive's own setting in force.
func WithPrerelease(allow *bool) Option {
	return func(c *config) { c.prerelease = allow }
}

// WithDeep enables a deep lookup: providers follow pagination to exhaustion
// instead of reading only the first (newest) page. More accurate, at the cost of
// more requests that may be slow or hit rate limits. The default is shallow.
func WithDeep(deep bool) Option { return func(c *config) { c.deep = deep } }

// WithTruncationSink sets a callback invoked with a resource's label when a
// shallow lookup stopped with more results available, so the caller can suggest
// a deep lookup. It may be called concurrently.
func WithTruncationSink(sink func(resource string)) Option {
	return func(c *config) { c.truncationSink = sink }
}

// WithWorkers sets how many markers resolve concurrently (default: NumCPU).
func WithWorkers(n int) Option { return func(c *config) { c.workers = n } }

// WithMaxSize sets the largest file the scan will read.
func WithMaxSize(n int64) Option { return func(c *config) { c.maxSize = n } }

// WithNow injects the reference time cooldown measures against, keeping a run
// deterministic. Unset, the current time is used.
func WithNow(t time.Time) Option { return func(c *config) { c.now = t } }

// WithIgnoreFiles sets the ignore-file names honoured during the walk (default:
// .gitignore).
func WithIgnoreFiles(names ...string) Option {
	return func(c *config) { c.ignoreFiles = names }
}

// Run scans roots for directives, resolves every marker it finds against its
// provider (or the marker it follows), and renders the resolved version onto
// each target line. It is the read-and-resolve keystone: it performs the file
// and network I/O but writes nothing - applying or reporting the results is the
// caller's choice. Results are grouped by file in path order, markers in line
// order, so the output is deterministic.
func Run(ctx context.Context, roots []string, opts ...Option) ([]FileResult, error) {
	p, files, scanned, err := build(ctx, roots, opts...)
	if err != nil {
		return nil, err
	}
	p.reporter.Discovered(scanned, len(files), comments(files))
	p.resolve(ctx)
	return p.group(files), nil
}

// comments totals the directives discovered across the scanned files.
func comments(files []scan.File) int {
	total := 0
	for _, f := range files {
		total += len(f.Found)
	}
	return total
}

// Validate is the offline counterpart of [Run]: it scans, binds, and checks
// every marker - that its provider and keys are valid, its target line carries
// an unambiguous version, its rule compiles, and its follow edges resolve -
// without any network or writes. It is the engine behind lint, surfacing each
// marker's own problem rather than cascading one failure into the next.
func Validate(ctx context.Context, roots []string, opts ...Option) ([]FileResult, error) {
	p, files, _, err := build(ctx, roots, opts...)
	if err != nil {
		return nil, err
	}
	p.validate(ctx)
	return p.group(files), nil
}

// Scan walks roots offline - honouring ignore files, never resolving - and
// returns the files that carry a directive. It is the front half Run and
// Validate build on, exposed for format mode, which rewrites directive comments
// without ever binding markers or touching the network.
func Scan(ctx context.Context, roots []string, opts ...Option) ([]scan.File, error) {
	_, files, _, err := scanRoots(ctx, roots, newConfig(opts...))
	return files, err
}

// newConfig applies opts over the defaults, clamping the worker count and
// defaulting the clock so cooldown has a reference time.
func newConfig(opts ...Option) config {
	cfg := config{workers: runtime.NumCPU(), reporter: progress.Nop{}}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.workers < 1 {
		cfg.workers = 1
	}
	if cfg.now.IsZero() {
		cfg.now = time.Now()
	}
	if cfg.reporter == nil {
		cfg.reporter = progress.Nop{}
	}
	return cfg
}

// scanRoots walks roots, pruning ignored paths, and returns the VCS resolver
// (for marker namespacing) alongside the files found.
func scanRoots(
	ctx context.Context,
	roots []string,
	cfg config,
) (*vcs.Resolver, []scan.File, int, error) {
	resolver := vcs.NewResolver()
	matcher := ignore.New(resolver, ignore.WithFiles(cfg.ignoreFiles...))

	scanOpts := []scan.Option{
		scan.WithWorkers(cfg.workers),
		scan.WithIgnore(ignoreFunc(matcher, cfg.exclude)),
	}
	if cfg.maxSize > 0 {
		scanOpts = append(scanOpts, scan.WithMaxSize(cfg.maxSize))
	}
	files, scanned, err := scan.Scan(ctx, roots, scanOpts...)
	return resolver, files, scanned, err
}

// ignoreFunc combines the ignore-file matcher with the configured exclude globs:
// a path is skipped if either rejects it.
func ignoreFunc(matcher *ignore.Matcher, exclude []string) func(string, bool) bool {
	return func(path string, isDir bool) bool {
		if matcher.Ignore(path, isDir) {
			return true
		}
		for _, glob := range exclude {
			if ok, _ := doublestar.Match(glob, path); ok {
				return true
			}
		}
		return false
	}
}

// build scans roots and binds the discovered directives into a plan ready for
// either resolution or validation.
func build(ctx context.Context, roots []string, opts ...Option) (*plan, []scan.File, int, error) {
	cfg := newConfig(opts...)
	resolver, files, scanned, err := scanRoots(ctx, roots, cfg)
	if err != nil {
		return nil, nil, 0, err
	}
	return newPlan(files, resolver, cfg), files, scanned, nil
}

// plan holds the state a run threads between seams: the flattened markers, each
// file's lines for rendering, the run-scoped registry follow markers read, the
// progress reporter, and one result slot per marker. Each task writes only its
// own slot, so the slice needs no lock - the same discipline the executor uses
// internally.
type plan struct {
	allowDowngrade *bool
	checksumClient *http.Client
	deep           bool
	lines          map[string][]string
	markers        []Marker
	now            time.Time
	prerelease     *bool
	registry       *registry.Registry
	reporter       progress.Reporter
	results        []Result
	tasks          []progress.Task
	truncationSink func(resource string)
	workers        int
}

// newPlan flattens the scanned files into markers and pre-seeds a result per
// marker, namespacing ids by repository so the same id in two repositories does
// not collide.
func newPlan(files []scan.File, resolver *vcs.Resolver, cfg config) *plan {
	lines := make(map[string][]string, len(files))
	var markers []Marker
	for _, f := range files {
		lines[f.Path] = f.Lines
		for _, m := range Markers(f, resolver) {
			if cfg.filter.Match(m.Tags) {
				markers = append(markers, m)
			}
		}
	}

	results := make([]Result, len(markers))
	for i, m := range markers {
		results[i] = Result{Marker: m, NewLine: targetLine(lines, m)}
	}

	return &plan{
		allowDowngrade: cfg.allowDowngrade,
		checksumClient: httpcache.New(),
		deep:           cfg.deep,
		lines:          lines,
		markers:        markers,
		now:            cfg.now,
		prerelease:     cfg.prerelease,
		registry:       registry.New(),
		reporter:       cfg.reporter,
		results:        results,
		truncationSink: cfg.truncationSink,
		workers:        cfg.workers,
	}
}

// resolve schedules every marker through the follow-edge executor, reporting
// each one's progress, then folds the executor's per-task verdict
// (skipped/errored) back onto each result. The closures report Done/Fail as each
// marker finishes; skipped markers never run a closure, so resolve reports their
// Skip here.
func (p *plan) resolve(ctx context.Context) {
	ctx = provider.WithDeep(ctx, p.deep)
	ctx = provider.WithTruncationSink(ctx, p.truncationSink)

	names := make([]string, len(p.markers))
	for i, m := range p.markers {
		names[i] = label(m)
	}
	tasks, wait := p.reporter.Begin(names)
	defer wait()
	p.tasks = tasks

	execTasks := make([]exec.Task, len(p.markers))
	for i, m := range p.markers {
		task := exec.Task{ID: m.ID, From: m.From, Label: bareID(m.ID), FromLabel: bareID(m.From)}
		if m.IsFollower() {
			task.Run = p.follower(i)
		} else {
			task.Run = p.producer(i)
		}
		execTasks[i] = task
	}

	for i, r := range exec.Execute(ctx, execTasks, p.workers) {
		switch {
		case r.Skipped:
			p.results[i].Skipped = true
			p.results[i].Reason = r.Reason
			p.tasks[i].Skip(r.Reason)
		case r.Err != nil:
			p.results[i].Err = r.Err
		}
	}
}

// producer returns the closure that resolves marker i from its upstream
// provider, reporting the outcome to the marker's progress task.
func (p *plan) producer(i int) func(context.Context) error {
	return func(ctx context.Context) error {
		p.tasks[i].Update("resolving")
		err := p.resolveProducer(ctx, i)
		p.report(i, err)
		return err
	}
}

// resolveProducer locates the current token, selects the newest allowed
// candidate, publishes it under the marker's id for followers, and renders it.
// lookupProvider resolves a marker's provider name, distinguishing an auto
// marker that could not be inferred from an outright unknown provider so the
// message points at the real fix.
func lookupProvider(name string) (provider.Provider, error) {
	if prov, ok := provider.Get(name); ok {
		return prov, nil
	}
	if name == constant.ProviderAuto {
		return nil, errors.New(
			"could not infer a provider for the target line; set provider= explicitly",
		)
	}
	return nil, fmt.Errorf("unknown provider %q", name)
}

func (p *plan) resolveProducer(ctx context.Context, i int) error {
	m := p.markers[i]

	prov, err := lookupProvider(m.Provider)
	if err != nil {
		return err
	}
	resource, err := prov.Resource(m.Directive)
	if err != nil {
		return err
	}

	line, rewriter, located, err := p.locate(m)
	if err != nil {
		return err
	}

	candidates, err := prov.Discover(ctx, resource)
	if err != nil {
		return err
	}

	opts, err := rule.Compile(m.Directive, located.Semver)
	if err != nil {
		return err
	}
	opts = append(opts, version.WithNow(p.now))
	// Run-level overrides are appended after the directive's own options, so a
	// non-nil flag wins over the per-directive rule.
	if p.allowDowngrade != nil {
		opts = append(opts, version.WithAllowDowngrade(*p.allowDowngrade))
	}
	if p.prerelease != nil {
		opts = append(opts, version.WithPrerelease(*p.prerelease))
	}

	chosen, ok := version.Select(located.Semver, candidates, attrs, opts...)
	if !ok {
		return ErrNoCandidate
	}

	// A secure pin (e.g. an image @sha256 digest) needs the chosen tag's digest,
	// resolved here for the winner only so discovery stays cheap.
	if located.NeedsDigest() {
		digester, isDigester := prov.(provider.Digester)
		if !isDigester {
			return fmt.Errorf("provider %q cannot resolve a digest for a pinned image", m.Provider)
		}
		digest, err := digester.Digest(ctx, resource, chosen.Version)
		if err != nil {
			return err
		}
		chosen.Digest = digest
	}

	if m.ID != "" {
		old := model.Candidate{Version: located.Raw, Semver: located.Semver}
		p.registry.Set(m.ID, registry.Entry{Old: old, New: chosen})
	}
	return p.render(i, line, rewriter, located, chosen)
}

// follower returns the closure that resolves marker i from the marker it
// follows, reporting the outcome to the marker's progress task.
func (p *plan) follower(i int) func(context.Context) error {
	return func(ctx context.Context) error {
		p.tasks[i].Update("following")
		err := p.resolveFollower(ctx, i)
		p.report(i, err)
		return err
	}
}

// resolveFollower projects the requested value from the producer marker i
// follows and renders it onto the target line. version and commit are direct
// projections; sha256 is fetched from the producer's version's checksum file.
func (p *plan) resolveFollower(ctx context.Context, i int) error {
	m := p.markers[i]

	resolved, err := p.followValue(ctx, m)
	if err != nil {
		return err
	}

	line, rewriter, located, err := p.locate(m)
	if err != nil {
		return err
	}

	// A follower carrying its own id republishes the producer it reads, so a
	// chain (A→B→C) resolves the version through every hop.
	if m.ID != "" {
		if entry, ok := p.registry.Get(m.From); ok {
			p.registry.Set(m.ID, entry)
		}
	}

	// The resolved value (a version, commit, or sha256) is rendered through the
	// rewriter seam: the smart rewriter for a version, the hash rewriter for a
	// commit or sha256.
	return p.render(i, line, rewriter, located, model.Candidate{Version: resolved})
}

// followValue computes a follower's value: version and commit are projected from
// the producer's candidate, while sha256 is fetched from the producer version's
// checksum file (tier one: an explicit sha256-url, templated with {version}).
func (p *plan) followValue(ctx context.Context, m Marker) (string, error) {
	if m.Value != constant.ValueSha256 {
		return follow.Resolve(p.registry, m.From, m.Value, m.Select)
	}

	version, err := follow.Resolve(p.registry, m.From, constant.ValueVersion, m.Select)
	if err != nil {
		return "", err
	}
	url, ok := m.Directive.Get(constant.DirectiveSha256URL)
	if !ok {
		return "", fmt.Errorf(
			"value=%s needs %s=",
			constant.ValueSha256,
			constant.DirectiveSha256URL,
		)
	}
	pat, _ := m.Directive.Get(constant.DirectivePattern)
	return checksum.Fetch(ctx, p.checksumClient, url, cversion.RemovePrefix(version), pat)
}

// report sends a marker's terminal progress event: the resolved value on
// success, or the error on failure.
func (p *plan) report(i int, err error) {
	if err != nil {
		p.tasks[i].Fail(err.Error())
		return
	}
	p.tasks[i].Done(p.results[i].Resolved)
}

// locate reads marker m's target line, selects the rewriter for it, and locates
// the current version, failing loud when the line is absent or the rewriter
// cannot act on it. The chosen rewriter is returned so Render reuses the same
// located spans and style.
func (p *plan) locate(m Marker) (string, match.Rewriter, match.Located, error) {
	line := targetLine(p.lines, m)
	if line == "" {
		return "", nil, match.Located{}, errors.New("no target line for directive")
	}

	rewriter := match.For(match.Context{
		Path:     m.File,
		Line:     line,
		Provider: m.Provider,
		Value:    m.Value,
	})
	located, err := rewriter.Locate(line)
	if err != nil {
		return "", nil, match.Located{}, err
	}
	return line, rewriter, located, nil
}

// render rewrites candidate onto the located target and records the result.
func (p *plan) render(
	i int,
	line string,
	rewriter match.Rewriter,
	located match.Located,
	candidate model.Candidate,
) error {
	newLine, changed, err := rewriter.Render(line, located, candidate)
	if err != nil {
		return err
	}
	p.results[i].Current = located.Raw
	p.results[i].Resolved = candidate.Version
	p.results[i].NewLine = newLine
	p.results[i].Changed = changed
	return nil
}

// group buckets the resolved markers back into their files, preserving file
// order (already path-sorted by the scan) and line order within each file.
func (p *plan) group(files []scan.File) []FileResult {
	byPath := make(map[string][]Result, len(files))
	for _, r := range p.results {
		byPath[r.Marker.File] = append(byPath[r.Marker.File], r)
	}

	out := make([]FileResult, 0, len(files))
	for _, f := range files {
		results, ok := byPath[f.Path]
		if !ok {
			continue
		}
		out = append(out, FileResult{Path: f.Path, Lines: f.Lines, Results: results})
	}
	return out
}

// targetLine returns marker m's target line, or "" when the directive has no
// line to rewrite (e.g. a trailing directive on the last line).
func targetLine(lines map[string][]string, m Marker) string {
	content, ok := lines[m.File]
	if !ok || m.Target < 0 || m.Target >= len(content) {
		return ""
	}
	return content[m.Target]
}

// attrs maps a discovered candidate onto the slice the selection chain reads.
func attrs(c model.Candidate) version.Attrs {
	return version.Attrs{Tag: c.Version, Semver: c.Semver, PublishedAt: c.PublishedAt}
}

// label is the progress display name for a marker: its file and directive line,
// where the user will see the change.
func label(m Marker) string {
	return fmt.Sprintf("%s:%d", filepath.Base(m.File), m.Line+1)
}
