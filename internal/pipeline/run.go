package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gechr/clover/internal/exec"
	"github.com/gechr/clover/internal/ignore"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/progress"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/follow"
	"github.com/gechr/clover/internal/registry"
	"github.com/gechr/clover/internal/rule"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/vcs"
	"github.com/gechr/clover/internal/version"
)

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
	workers     int
	maxSize     int64
	now         time.Time
	ignoreFiles []string
	reporter    progress.Reporter
}

// Option configures [Run].
type Option func(*config)

// WithReporter sets the progress reporter that observes markers as they resolve.
// The default discards everything; the CLI supplies a live display.
func WithReporter(r progress.Reporter) Option {
	return func(c *config) { c.reporter = r }
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
	p, files, err := build(ctx, roots, opts...)
	if err != nil {
		return nil, err
	}
	p.resolve(ctx, p.workers)
	return p.group(files), nil
}

// Validate is the offline counterpart of [Run]: it scans, binds, and checks
// every marker - that its provider and keys are valid, its target line carries
// an unambiguous version, its rule compiles, and its follow edges resolve -
// without any network or writes. It is the engine behind lint, surfacing each
// marker's own problem rather than cascading one failure into the next.
func Validate(ctx context.Context, roots []string, opts ...Option) ([]FileResult, error) {
	p, files, err := build(ctx, roots, opts...)
	if err != nil {
		return nil, err
	}
	p.validate(ctx, p.workers)
	return p.group(files), nil
}

// Scan walks roots offline - honouring ignore files, never resolving - and
// returns the files that carry a directive. It is the front half Run and
// Validate build on, exposed for format mode, which rewrites directive comments
// without ever binding markers or touching the network.
func Scan(ctx context.Context, roots []string, opts ...Option) ([]scan.File, error) {
	_, files, err := scanRoots(ctx, roots, newConfig(opts...))
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
) (*vcs.Resolver, []scan.File, error) {
	resolver := vcs.NewResolver()
	matcher := ignore.New(resolver, ignore.WithFiles(cfg.ignoreFiles...))

	scanOpts := []scan.Option{
		scan.WithWorkers(cfg.workers),
		scan.WithIgnore(matcher.Ignore),
	}
	if cfg.maxSize > 0 {
		scanOpts = append(scanOpts, scan.WithMaxSize(cfg.maxSize))
	}
	files, err := scan.Scan(ctx, roots, scanOpts...)
	return resolver, files, err
}

// build scans roots and binds the discovered directives into a plan ready for
// either resolution or validation.
func build(ctx context.Context, roots []string, opts ...Option) (*plan, []scan.File, error) {
	cfg := newConfig(opts...)
	resolver, files, err := scanRoots(ctx, roots, cfg)
	if err != nil {
		return nil, nil, err
	}
	return newPlan(files, resolver, cfg), files, nil
}

// plan holds the state a run threads between seams: the flattened markers, each
// file's lines for rendering, the run-scoped registry follow markers read, the
// progress reporter, and one result slot per marker. Each task writes only its
// own slot, so the slice needs no lock - the same discipline the executor uses
// internally.
type plan struct {
	markers  []Marker
	lines    map[string][]string
	registry *registry.Registry
	smart    match.Smart
	now      time.Time
	workers  int
	reporter progress.Reporter
	tasks    []progress.Task
	results  []Result
}

// newPlan flattens the scanned files into markers and pre-seeds a result per
// marker, namespacing ids by repository so the same id in two repositories does
// not collide.
func newPlan(files []scan.File, resolver *vcs.Resolver, cfg config) *plan {
	lines := make(map[string][]string, len(files))
	var markers []Marker
	for _, f := range files {
		lines[f.Path] = f.Lines
		markers = append(markers, Markers(f, resolver)...)
	}

	results := make([]Result, len(markers))
	for i, m := range markers {
		results[i] = Result{Marker: m, NewLine: targetLine(lines, m)}
	}

	return &plan{
		markers:  markers,
		lines:    lines,
		registry: registry.New(),
		smart:    match.NewSmart(),
		now:      cfg.now,
		workers:  cfg.workers,
		reporter: cfg.reporter,
		results:  results,
	}
}

// resolve schedules every marker through the follow-edge executor, reporting
// each one's progress, then folds the executor's per-task verdict
// (skipped/errored) back onto each result. The closures report Done/Fail as each
// marker finishes; skipped markers never run a closure, so resolve reports their
// Skip here.
func (p *plan) resolve(ctx context.Context, workers int) {
	names := make([]string, len(p.markers))
	for i, m := range p.markers {
		names[i] = label(m)
	}
	tasks, wait := p.reporter.Begin(names)
	defer wait()
	p.tasks = tasks

	execTasks := make([]exec.Task, len(p.markers))
	for i, m := range p.markers {
		task := exec.Task{ID: m.ID, From: m.From}
		if m.IsFollower() {
			task.Run = p.follower(i)
		} else {
			task.Run = p.producer(i)
		}
		execTasks[i] = task
	}

	for i, r := range exec.Execute(ctx, execTasks, workers) {
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
func (p *plan) resolveProducer(ctx context.Context, i int) error {
	m := p.markers[i]

	prov, ok := provider.Get(m.Provider)
	if !ok {
		return fmt.Errorf("unknown provider %q", m.Provider)
	}
	resource, err := prov.Resource(m.Directive)
	if err != nil {
		return err
	}

	line, current, token, err := p.locate(m)
	if err != nil {
		return err
	}

	candidates, err := prov.Discover(ctx, resource)
	if err != nil {
		return err
	}

	opts, err := rule.Compile(m.Directive, current)
	if err != nil {
		return err
	}
	opts = append(opts, version.WithNow(p.now))

	chosen, ok := version.Select(current, candidates, attrs, opts...)
	if !ok {
		return fmt.Errorf("no candidate satisfies the rule")
	}

	if m.ID != "" {
		old := model.Candidate{Version: token.Current, Semver: current}
		p.registry.Set(m.ID, registry.Entry{Old: old, New: chosen})
	}
	p.render(i, line, token, chosen.Version)
	return nil
}

// follower returns the closure that resolves marker i from the marker it
// follows, reporting the outcome to the marker's progress task.
func (p *plan) follower(i int) func(context.Context) error {
	return func(_ context.Context) error {
		p.tasks[i].Update("following")
		err := p.resolveFollower(i)
		p.report(i, err)
		return err
	}
}

// resolveFollower projects the requested value from the producer marker i
// follows and renders it onto the target line.
func (p *plan) resolveFollower(i int) error {
	m := p.markers[i]

	resolved, err := follow.Resolve(p.registry, m.From, m.Value, m.Select)
	if err != nil {
		return err
	}

	line, _, token, err := p.locate(m)
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
	p.render(i, line, token, resolved)
	return nil
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

// located carries the current token's text alongside the parsed token, so a
// caller has both the raw string (for the old value) and its decomposed parts.
type located struct {
	match.Token

	Current string
}

// locate reads marker m's target line and finds the single version token on it,
// failing loud when the line is absent or the token is ambiguous.
func (p *plan) locate(m Marker) (string, *version.Version, located, error) {
	line := targetLine(p.lines, m)
	if line == "" {
		return "", nil, located{}, fmt.Errorf("no target line for directive")
	}

	token, ok := p.smart.Locate(line)
	if !ok {
		return "", nil, located{}, fmt.Errorf("ambiguous or missing version on target line")
	}

	current, err := version.Parse(token.Core)
	if err != nil {
		current = nil // an unparseable core only matters to a keyword constraint
	}
	return line, current, located{Token: token, Current: line[token.Span.Start:token.Span.End]}, nil
}

// render restyles resolved onto the located token and records the result.
func (p *plan) render(i int, line string, token located, resolved string) {
	newLine, changed := p.smart.Render(line, token.Token, resolved)
	p.results[i].Current = token.Current
	p.results[i].Resolved = resolved
	p.results[i].NewLine = newLine
	p.results[i].Changed = changed
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
