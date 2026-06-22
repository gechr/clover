package pipeline

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	cversion "github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/checksum"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/exec"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/ignore"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pattern"
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
	Written  string // the value rendered onto the line; what the report shows as `to`
	NewLine  string // the target line after rendering, == original when unchanged
	Changed  bool   // whether rendering altered the target line
	Skipped  bool   // the marker's dependency failed, was missing, or cycled
	Reason   string // why the marker was skipped
	Err      error  // why resolution failed
	Verify   error  // a secure pin failed verification (non-fatal: the marker still resolved)
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
	downgrade      *bool
	deep           bool
	exclude        []string
	filter         tag.Filter
	ignoreFiles    []string
	maxSize        int64
	now            time.Time
	prerelease     *bool
	reporter       progress.Reporter
	truncationSink func(resource string)
	verify         *bool
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

// WithDowngrade overrides the per-directive downgrade rule for every
// marker: a non-nil allow forces downgrades on or off run-wide, while nil leaves
// each directive's own setting in force.
func WithDowngrade(allow *bool) Option {
	return func(c *config) { c.downgrade = allow }
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

// WithVerify overrides the per-directive verify rule for every marker: a non-nil
// value forces the deep tag-on-branch check on or off run-wide, while nil leaves
// each directive's own verify/verify-branch setting in force.
func WithVerify(on *bool) Option { return func(c *config) { c.verify = on } }

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
		total += len(f.Found) + len(f.Errors)
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
	exclude = validExcludes(exclude)
	return func(path string, isDir bool) bool {
		for _, glob := range exclude {
			if doublestar.MatchUnvalidated(glob, path) {
				return true
			}
		}
		return matcher.Ignore(path, isDir)
	}
}

func validExcludes(globs []string) []string {
	valid := make([]string, 0, len(globs))
	for _, glob := range globs {
		if doublestar.ValidatePattern(glob) {
			valid = append(valid, glob)
		}
	}
	return valid
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
	downgrade      *bool
	checksumClient *http.Client
	deep           bool
	lines          map[string][]string
	markers        []Marker
	now            time.Time
	parseErrors    []Result
	prerelease     *bool
	registry       *registry.Registry
	reporter       progress.Reporter
	results        []Result
	tasks          []progress.Task
	truncationSink func(resource string)
	verify         *bool
	workers        int
}

// newPlan flattens the scanned files into markers and pre-seeds a result per
// marker, namespacing ids by repository so the same id in two repositories does
// not collide.
func newPlan(files []scan.File, resolver *vcs.Resolver, cfg config) *plan {
	lines := make(map[string][]string, len(files))
	var markers []Marker
	var parseErrors []Result
	for _, f := range files {
		lines[f.Path] = f.Lines
		parseErrors = append(parseErrors, parseErrorResults(f)...)
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
		downgrade:      cfg.downgrade,
		checksumClient: httpcache.New(),
		deep:           cfg.deep,
		lines:          lines,
		markers:        markers,
		now:            cfg.now,
		parseErrors:    parseErrors,
		prerelease:     cfg.prerelease,
		registry:       registry.New(),
		reporter:       cfg.reporter,
		results:        results,
		truncationSink: cfg.truncationSink,
		verify:         cfg.verify,
		workers:        cfg.workers,
	}
}

func parseErrorResults(f scan.File) []Result {
	results := make([]Result, 0, len(f.Errors))
	for _, e := range f.Errors {
		marker := Marker{File: f.Path, Line: e.Line, Target: e.Line}
		results = append(
			results,
			Result{Marker: marker, NewLine: lineAt(f.Lines, e.Line), Err: e.Err},
		)
	}
	return results
}

func lineAt(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
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
		return nil, fmt.Errorf(
			"could not infer a provider for the target line; set %s= explicitly",
			constant.DirectiveProvider,
		)
	}
	return nil, fmt.Errorf("unknown provider %q", name)
}

func (p *plan) resolveProducer(ctx context.Context, i int) error {
	m := p.markers[i]

	if m.Directive.Has(constant.DirectiveTrack) {
		if err := trackPreconditions(m); err != nil {
			return err
		}
	}

	prov, err := lookupProvider(m.Provider)
	if err != nil {
		return err
	}
	resource, err := prov.Resource(m.Directive)
	if err != nil {
		return err
	}

	line, located, err := p.locate(m)
	if err != nil {
		return err
	}

	// A track= marker follows a floating ref directly, skipping the discover and
	// select stages that only make sense for a semver candidate set.
	if m.Directive.Has(constant.DirectiveTrack) {
		return p.resolveTrack(ctx, i, m, prov, resource, line, located)
	}

	candidates, err := prov.Discover(ctx, resource)
	if err != nil {
		return err
	}

	opts, err := rule.Compile(m.Directive, located.Semver())
	if err != nil {
		return err
	}
	if opt, ok := variantInclude(located.Current(), m.Directive); ok {
		opts = append(opts, opt)
	}
	opts = append(opts, version.WithNow(p.now))
	// Run-level overrides are appended after the directive's own options, so a
	// non-nil flag wins over the per-directive rule.
	if p.downgrade != nil {
		opts = append(opts, version.WithDowngrade(*p.downgrade))
	}
	if p.prerelease != nil {
		opts = append(opts, version.WithPrerelease(*p.prerelease))
	}
	// Surface why each candidate was passed over, visible under --verbose. Only
	// wire the observer when debug is on, so the common path does no work per
	// rejected candidate.
	if clog.GetLevel() <= clog.LevelDebug {
		res := fmt.Sprintf("%v", resource)
		opts = append(opts, version.WithObserver(func(tag string, r version.Reason) {
			clog.Debug().
				Str(field.Resource, res).
				Str(field.Version, tag).
				Str(field.Reason, r.String()).
				Msg("Skipped candidate")
		}))
	}

	// A monorepo tag-prefix scopes selection to one component's tags
	// (api/v1.4.0), parsing and ordering on the version after the prefix; the
	// prefix is stripped from the winner so everything downstream - render, the
	// published value, a digest - sees the bare version.
	extract := attrs
	prefix, _ := m.Directive.Get(constant.RuleTagPrefix)
	if prefix != "" {
		extract = prefixedAttrs(prefix)
	}

	chosen, ok := version.Select(located.Semver(), candidates, extract, opts...)
	if !ok {
		return ErrNoCandidate
	}
	if prefix != "" {
		chosen.Version = strings.TrimPrefix(chosen.Version, prefix)
	}

	// A secure pin (e.g. an image @sha256 digest) needs the chosen tag's digest,
	// resolved here for the winner only so discovery stays cheap. Resolve it for
	// the tag actually rendered (restyle can strip a variant or re-precision the
	// core), so the pinned digest always describes the tag written, not the raw
	// candidate.
	if located.NeedsDigest() {
		digester, isDigester := prov.(provider.Digester)
		if !isDigester {
			return fmt.Errorf("provider %q cannot resolve a digest for a pinned image", m.Provider)
		}
		tag := chosen.Version
		if r, ok := located.(match.Rendered); ok {
			tag = r.Rendered(chosen)
		}
		digest, err := digester.Digest(ctx, resource, tag)
		if err != nil {
			return err
		}
		chosen.Digest = digest
	}

	return p.finalize(ctx, i, m, prov, resource, line, located, chosen)
}

// resolveTrack resolves a track= marker by following its floating ref directly:
// it takes the ref named on the directive (or, for track=*, the one already on
// the line), gates adoption by cooldown, resolves the ref's secure value - a
// docker content digest or a github branch-head commit - and renders it. It
// never runs selection, so the ref's tag/branch text is preserved as written.
func (p *plan) resolveTrack(
	ctx context.Context,
	i int,
	m Marker,
	prov provider.Provider,
	resource provider.Resource,
	line string,
	located match.Located,
) error {
	ref, _ := m.Directive.Get(constant.DirectiveTrack)
	if ref == constant.TrackInfer {
		ref = located.Current()
	}
	chosen := model.Candidate{Version: ref, Ref: ref}

	if err := p.trackCooldown(ctx, m, prov, resource, ref); err != nil {
		return err
	}

	switch {
	case located.NeedsDigest():
		digester, ok := prov.(provider.Digester)
		if !ok {
			return fmt.Errorf("provider %q cannot resolve a digest for a tracked tag", m.Provider)
		}
		digest, err := digester.Digest(ctx, resource, ref)
		if err != nil {
			return err
		}
		chosen.Digest = digest
	default:
		committer, ok := prov.(provider.Committer)
		if !ok {
			return fmt.Errorf(
				"provider %q cannot resolve a commit for a tracked branch",
				m.Provider,
			)
		}
		commit, err := committer.Commit(ctx, resource, ref)
		if err != nil {
			return err
		}
		chosen.Commit = commit
	}

	return p.finalize(ctx, i, m, prov, resource, line, located, chosen)
}

// trackCooldown rejects a tracked ref whose current target is younger than the
// marker's cooldown=, so a too-fresh digest or commit is not adopted yet. It is
// inert without a cooldown, and where the provider lists no publish time for the
// ref (an OCI registry, a github branch), matching selection's best-effort rule.
func (p *plan) trackCooldown(
	ctx context.Context,
	m Marker,
	prov provider.Provider,
	resource provider.Resource,
	ref string,
) error {
	cooldown, err := m.Directive.Duration(constant.RuleCooldown)
	if err != nil {
		return err
	}
	if cooldown <= 0 {
		return nil
	}
	candidates, err := prov.Discover(ctx, resource)
	if err != nil {
		return err
	}
	for _, c := range candidates {
		if c.Version == ref && version.TooFresh(p.now, c.PublishedAt, cooldown) {
			return ErrNoCandidate
		}
	}
	return nil
}

// finalize publishes the resolved candidate (when the marker carries an id),
// renders it onto the line, and runs the secure-pin cross-checks. Shared by the
// select and track resolution paths.
func (p *plan) finalize(
	ctx context.Context,
	i int,
	m Marker,
	prov provider.Provider,
	resource provider.Resource,
	line string,
	located match.Located,
	chosen model.Candidate,
) error {
	if m.ID != "" {
		old := model.Candidate{Version: located.Current(), Semver: located.Semver()}
		p.registry.Set(m.ID, registry.Entry{Old: old, New: chosen})
	}
	if err := p.render(i, line, located, chosen); err != nil {
		return err
	}
	p.results[i].Verify = verifyPin(located, chosen)
	if p.results[i].Verify == nil && p.deepVerify(m) {
		p.results[i].Verify = p.verifyBranch(ctx, prov, resource, located, chosen, m)
	}
	return nil
}

// deepVerify reports whether the deep tag-on-branch check runs for marker m: the
// --verify run flag overrides, else the marker's own verify=/verify-branch=.
func (p *plan) deepVerify(m Marker) bool {
	if p.verify != nil {
		return *p.verify
	}
	on, _ := m.Directive.Bool(constant.DirectiveVerify)
	return on || m.Directive.Has(constant.DirectiveVerifyBranch)
}

// verifyBranch confirms the commit clover resolved for a secure pin is reachable
// from an allowed branch (default: the repo's default branch; or the
// verify-branch= regex), guarding against a tag that points at an off-trunk
// commit. It fails closed. Digest pins and providers without branch provenance
// are skipped, as is a marker whose target is not a secure pin.
func (p *plan) verifyBranch(
	ctx context.Context,
	prov provider.Provider,
	resource provider.Resource,
	located match.Located,
	chosen model.Candidate,
	m Marker,
) error {
	if located.NeedsDigest() {
		return nil // a registry digest has no branch provenance
	}
	if _, ok := located.(match.SecurePin); !ok {
		return nil
	}
	checker, ok := prov.(provider.BranchChecker)
	if !ok {
		return nil
	}

	commit := chosen.Commit
	if commit == "" { // the tag was off the discovered page; resolve it directly
		committer, ok := prov.(provider.Committer)
		if !ok {
			return nil
		}
		c, err := committer.Commit(ctx, resource, located.Current())
		if err != nil {
			return fmt.Errorf("verify: %w", err)
		}
		commit = c
	}
	if commit == "" {
		return nil
	}

	branches, err := p.allowedBranches(ctx, checker, resource, m)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	for _, b := range branches {
		if b.Tip == commit { // fast path: the commit is the branch tip
			return nil
		}
		reachable, err := checker.Reachable(ctx, resource, b.Name, commit)
		if err != nil {
			return fmt.Errorf("verify: %w", err)
		}
		if reachable {
			return nil
		}
	}
	return fmt.Errorf(
		"commit %s for %s is not on %s",
		commit, chosen.Version, branchDescription(m),
	)
}

// allowedBranches resolves the branches a tag's commit may be reached from: the
// verify-branch= value (a literal name, or a regex matched against the listed
// branches), else the repo's default branch.
func (p *plan) allowedBranches(
	ctx context.Context,
	checker provider.BranchChecker,
	resource provider.Resource,
	m Marker,
) ([]provider.Branch, error) {
	raw, ok := m.Directive.Get(constant.DirectiveVerifyBranch)
	if !ok || raw == "" {
		def, err := checker.DefaultBranch(ctx, resource)
		if err != nil {
			return nil, err
		}
		return []provider.Branch{{Name: def}}, nil
	}

	// verify-branch is a glob by default, or a /regex/ - the same matcher
	// include/exclude use, so the spelling is consistent across directives.
	pat, err := pattern.Compile(raw)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", constant.DirectiveVerifyBranch, raw, err)
	}
	branches, err := checker.Branches(ctx, resource)
	if err != nil {
		return nil, err
	}
	var matched []provider.Branch
	for _, b := range branches {
		if pat.Matches(b.Name) {
			matched = append(matched, b)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no branch matches %s=%s", constant.DirectiveVerifyBranch, raw)
	}
	return matched, nil
}

// branchDescription names the allowed branches for an error message: the
// verify-branch= pattern when set, else a plain phrase for the default branch.
func branchDescription(m Marker) string {
	if pattern, ok := m.Directive.Get(constant.DirectiveVerifyBranch); ok {
		return "an allowed branch (" + constant.DirectiveVerifyBranch + "=" + pattern + ")"
	}
	return "an allowed branch"
}

// verifyPin cross-checks an up-to-date secure pin against the resolved tag: the
// committed commit SHA (action pin) or content digest (image pin) must equal the
// one upstream reports for that version. It returns nil - the cheap, always-on
// check does nothing - when the target is not a secure pin, when the version is
// being bumped (the new pin is correct by construction, and the old version's tag
// may be off the fetched page), or when there is no upstream value to compare. A
// non-nil result is non-fatal: the marker still resolved.
func verifyPin(located match.Located, chosen model.Candidate) error {
	pin, ok := located.(match.SecurePin)
	if !ok {
		return nil
	}
	if located.Semver() == nil || chosen.Semver == nil || !located.Semver().Equal(chosen.Semver) {
		return nil
	}

	upstream := chosen.Commit
	if located.NeedsDigest() {
		upstream = chosen.Digest
	}
	if upstream == "" || pin.Pinned() == upstream {
		return nil
	}
	return fmt.Errorf(
		"pinned %s but %s upstream is %s",
		pin.Pinned(), chosen.Version, upstream,
	)
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

	// A track= marker carries no provider, so it lands here misclassified as a
	// follower; its preconditions reject it with the actionable error.
	if m.Directive.Has(constant.DirectiveTrack) {
		if err := trackPreconditions(m); err != nil {
			return err
		}
	}

	resolved, err := p.followValue(ctx, m)
	if err != nil {
		return err
	}

	line, located, err := p.locate(m)
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
	// commit or sha256. The candidate is typed by the follower's value so a
	// find/replace template's <commit>/<sha256>/<major.minor> tokens resolve.
	return p.render(i, line, located, followerCandidate(m.Value, resolved))
}

// followerCandidate wraps a follower's resolved value in a Candidate typed by the
// projected value kind. Version is always set so the hash rewriter (which
// splices Candidate.Version) keeps working.
func followerCandidate(value, resolved string) model.Candidate {
	c := model.Candidate{Version: resolved}
	switch value {
	case constant.ValueCommit:
		c.Commit = resolved
	case constant.ValueSha256:
		c.Digest = constant.DigestSha256 + resolved
	default:
		c.Semver, _ = version.Parse(resolved)
	}
	return c
}

// followValue computes a follower's value: version and commit are projected from
// the producer's candidate, while sha256 is fetched from the producer version's
// checksum file (tier one: an explicit sha256-url, templated with {version}).
func (p *plan) followValue(ctx context.Context, m Marker) (string, error) {
	if m.Value != constant.ValueSha256 {
		return follow.Resolve(p.registry, m.From, m.Value, m.Select)
	}

	cand, err := follow.Candidate(p.registry, m.From, m.Select)
	if err != nil {
		return "", err
	}
	url, _ := m.Directive.Get(constant.DirectiveSha256URL)
	pat, _ := m.Directive.Get(constant.DirectivePattern)
	source, _ := m.Directive.Get(constant.DirectiveSha256Source)
	return checksum.Resolve(ctx, p.checksumClient, checksum.Request{
		Source:  source,
		Assets:  cand.Assets,
		Pattern: pat,
		URL:     url,
		Version: cversion.RemovePrefix(cand.Version),
	})
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
func (p *plan) locate(m Marker) (string, match.Located, error) {
	line := targetLine(p.lines, m)
	if line == "" {
		return "", nil, errors.New("no target line for directive")
	}

	rewriter, err := rewriterFor(m, line)
	if err != nil {
		return "", nil, err
	}
	located, err := rewriter.Locate(line)
	if err != nil {
		return "", nil, err
	}
	return line, located, nil
}

// rewriterFor chooses the rewriter: a track= marker uses its provider's
// floating-ref rewriter, an explicit find/replace (find required, replace
// optional) overrides the shape-based dispatch, else the route table decides.
func rewriterFor(m Marker, line string) (match.Rewriter, error) {
	if m.Directive.Has(constant.DirectiveTrack) {
		switch m.Provider {
		case constant.ProviderDocker:
			return match.NewDockerTrack(), nil
		case constant.ProviderGithub:
			return match.NewActionTrack(), nil
		default:
			return nil, fmt.Errorf("track= is not supported for provider %q", m.Provider)
		}
	}

	find, hasFind := m.Directive.Get(constant.DirectiveFind)
	replace, hasReplace := m.Directive.Get(constant.DirectiveReplace)
	if !hasFind {
		if hasReplace {
			return nil, fmt.Errorf(
				"%s= needs %s=",
				constant.DirectiveReplace,
				constant.DirectiveFind,
			)
		}
		return match.For(match.Context{
			Path:     m.File,
			Line:     line,
			Provider: m.Provider,
			Value:    m.Value,
		}), nil
	}
	return match.NewFindReplace(find, replace)
}

// render rewrites candidate onto the located target and records the result.
func (p *plan) render(
	i int,
	line string,
	located match.Located,
	candidate model.Candidate,
) error {
	newLine, changed, err := located.Render(line, candidate)
	if err != nil {
		return err
	}
	p.results[i].Current = located.Current()
	p.results[i].Resolved = candidate.Version
	p.results[i].Written = renderedValue(located, candidate)
	p.results[i].NewLine = newLine
	p.results[i].Changed = changed
	return nil
}

// renderedValue is the version text actually written onto the line, which a
// restyle can shift from candidate.Version (a stripped variant, a re-precisioned
// core). A located that cannot report it falls back to the resolved value, so a
// follower projecting its value verbatim is unaffected.
func renderedValue(located match.Located, candidate model.Candidate) string {
	if r, ok := located.(match.Rendered); ok {
		return r.Rendered(candidate)
	}
	return candidate.Version
}

// group buckets the resolved markers back into their files, preserving file
// order (already path-sorted by the scan) and line order within each file.
func (p *plan) group(files []scan.File) []FileResult {
	byPath := make(map[string][]Result, len(files))
	for _, r := range p.parseErrors {
		byPath[r.Marker.File] = append(byPath[r.Marker.File], r)
	}
	for _, r := range p.results {
		byPath[r.Marker.File] = append(byPath[r.Marker.File], r)
	}

	out := make([]FileResult, 0, len(files))
	for _, f := range files {
		results, ok := byPath[f.Path]
		if !ok {
			continue
		}
		slices.SortFunc(results, func(a, b Result) int {
			return cmp.Compare(resultLine(a), resultLine(b))
		})
		out = append(out, FileResult{Path: f.Path, Lines: f.Lines, Results: results})
	}
	return out
}

func resultLine(r Result) int {
	if r.Marker.Line >= 0 {
		return r.Marker.Line
	}
	return r.Marker.Target
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

// prefixedAttrs maps a candidate by stripping a monorepo tag prefix (api/) before
// parsing, so a component-scoped tag (api/v1.4.0) selects on its version
// (v1.4.0). A candidate lacking the prefix yields a nil Semver and is dropped, so
// only the named component's tags are considered.
func prefixedAttrs(prefix string) func(model.Candidate) version.Attrs {
	return func(c model.Candidate) version.Attrs {
		rest, ok := strings.CutPrefix(c.Version, prefix)
		if !ok {
			return version.Attrs{Tag: c.Version}
		}
		semver, _ := version.Parse(rest)
		return version.Attrs{Tag: rest, Semver: semver, PublishedAt: c.PublishedAt}
	}
}

// variantInclude returns an include option restricting selection to candidates
// whose variant matches the located tag's: a variant line (1.27-alpine) keeps
// only that variant (*-alpine), and a plain line (1.27) keeps only plain tags.
// The rewriter re-applies the located suffix on render, so without this rule a
// plain 1.27 could pick 1.31-trixie's candidate and render 1.31 while pinning
// the variant's digest - a tag and digest that disagree. An explicit directive
// include takes precedence, so the rule stays in the user's hands.
func variantInclude(raw string, d directive.Directive) (version.Option, bool) {
	if d.Has(constant.RuleInclude) {
		return nil, false
	}
	_, want := version.SplitVariant(raw)
	return version.WithInclude(func(tag string) bool {
		_, got := version.SplitVariant(tag)
		return got == want
	}), true
}

// label is the progress display name for a marker: its file and directive line,
// where the user will see the change.
func label(m Marker) string {
	return fmt.Sprintf("%s:%d", filepath.Base(m.File), m.Line+1)
}
