package pipeline

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/gechr/cusp/internal/exec"
	"github.com/gechr/cusp/internal/ignore"
	"github.com/gechr/cusp/internal/match"
	"github.com/gechr/cusp/internal/model"
	"github.com/gechr/cusp/internal/provider"
	"github.com/gechr/cusp/internal/provider/follow"
	"github.com/gechr/cusp/internal/registry"
	"github.com/gechr/cusp/internal/rule"
	"github.com/gechr/cusp/internal/scan"
	"github.com/gechr/cusp/internal/vcs"
	"github.com/gechr/cusp/internal/version"
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
}

// Option configures [Run].
type Option func(*config)

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
	cfg := config{workers: runtime.NumCPU(), maxSize: 0}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.workers < 1 {
		cfg.workers = 1
	}
	if cfg.now.IsZero() {
		cfg.now = time.Now()
	}

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
	if err != nil {
		return nil, err
	}

	p := newPlan(files, resolver, cfg.now)
	p.resolve(ctx, cfg.workers)
	return p.group(files), nil
}

// plan holds the state a run threads between seams: the flattened markers, each
// file's lines for rendering, the run-scoped registry follow markers read, and
// one result slot per marker. Each task writes only its own slot, so the slice
// needs no lock - the same discipline the executor uses internally.
type plan struct {
	markers  []Marker
	lines    map[string][]string
	registry *registry.Registry
	smart    match.Smart
	now      time.Time
	results  []Result
}

// newPlan flattens the scanned files into markers and pre-seeds a result per
// marker, namespacing ids by repository so the same id in two repositories does
// not collide.
func newPlan(files []scan.File, resolver *vcs.Resolver, now time.Time) *plan {
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
		now:      now,
		results:  results,
	}
}

// resolve schedules every marker through the follow-edge executor, then folds
// the executor's per-task verdict (skipped/errored) back onto each result.
func (p *plan) resolve(ctx context.Context, workers int) {
	tasks := make([]exec.Task, len(p.markers))
	for i, m := range p.markers {
		task := exec.Task{ID: m.ID, From: m.From}
		if m.IsFollower() {
			task.Run = p.follower(i)
		} else {
			task.Run = p.producer(i)
		}
		tasks[i] = task
	}

	for i, r := range exec.Execute(ctx, tasks, workers) {
		switch {
		case r.Skipped:
			p.results[i].Skipped = true
			p.results[i].Reason = r.Reason
		case r.Err != nil:
			p.results[i].Err = r.Err
		}
	}
}

// producer returns the closure that resolves marker i from its upstream
// provider: locate the current token, select the newest allowed candidate, and
// publish it under the marker's id so followers can reuse it.
func (p *plan) producer(i int) func(context.Context) error {
	m := p.markers[i]
	return func(ctx context.Context) error {
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
}

// follower returns the closure that resolves marker i from the marker it
// follows, projecting the requested value and rendering it onto the target line.
func (p *plan) follower(i int) func(context.Context) error {
	m := p.markers[i]
	return func(context.Context) error {
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
