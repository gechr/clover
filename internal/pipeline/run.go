package pipeline

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	cversion "github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/checksum"
	"github.com/gechr/clover/internal/config"
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
	"github.com/gechr/clover/internal/vcs"
	"github.com/gechr/clover/internal/version"
	"github.com/gechr/x/ptr"
	"github.com/gechr/x/set"
)

// errCooldownUnsupported marks a producer skipped, not failed: its cooldown
// cannot be honored because the source returned no publication dates to measure
// age against. clover holds the line rather than blindly updating past a
// cooldown it cannot check.
var errCooldownUnsupported = errors.New(
	"cooldown not supported - source does not provide publication dates",
)

// ErrNoCandidate reports that no discovered version satisfied the marker's rule
// (constraint, include/exclude, prerelease). The edge checks for it to suggest a
// deep lookup, since a shallow lookup may have paged past a matching version.
var ErrNoCandidate = errors.New("no candidate satisfies the rule")

// Result is the outcome of resolving one marker: the version it found in the
// file, the value it resolved to, and the rewritten target line. Exactly one of
// a clean resolution, Skipped, Disabled, or Err holds. A skipped, disabled, or
// errored marker leaves Resolved empty and NewLine equal to the original line.
type Result struct {
	Marker   Marker
	Current  string // the version token currently on the target line
	Resolved string // the value the marker resolved to upstream, or via follow
	Written  string // the value rendered onto the line; what the report shows as `to`
	NewLine  string // the target line after rendering, == original when unchanged
	Changed  bool   // whether rendering altered the target line
	Skipped  bool   // the marker's dependency failed, was missing, or cycled
	Disabled bool   // the directive set disabled=...; the marker is intentionally inert (never a lint failure)
	Reason   string // why the marker was skipped, or the disabled= reason it was disabled with
	Err      error  // why resolution failed
	Verify   error  // a secure pin failed verification (non-fatal: the marker still resolved)
	Moved    string // the upstream commit a held pin's tag moved to; empty when unmoved

	// Truncated reports that a shallow lookup against a recency-ordered provider
	// stopped with more pages available. Paired with an ErrNoCandidate failure it
	// drives the gated --deep hint; it stays false for a lexically ordered
	// provider, whose truncation feeds the run-wide blanket hint instead.
	Truncated bool

	// ResolvedURL is the upstream web page for the resolved candidate (e.g. a
	// GitHub release/tag page), when the provider supplies one. The report
	// hyperlinks the reported version to it; empty when unavailable.
	ResolvedURL string

	// CurrentURL is the upstream web page for the version currently on the line,
	// with its ref inferred from the resolved ref's format. The report
	// hyperlinks the from value to it; empty when unavailable.
	CurrentURL string
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
// returns the files that carry a directive, alongside the total number of files
// examined. It is the front half Run and Validate build on, exposed for format
// and annotate, which rewrite directive comments without ever binding markers or
// touching the network.
func Scan(ctx context.Context, roots []string, opts ...Option) ([]scan.File, int, error) {
	_, files, scanned, err := scanRoots(ctx, roots, newSettings(opts...))
	return files, scanned, err
}

// scanRoots walks roots, pruning ignored paths, then applies each repository's
// required-version gate, and returns the VCS resolver (for marker namespacing)
// alongside the surviving files found.
func scanRoots(
	ctx context.Context,
	roots []string,
	set settings,
) (*vcs.Resolver, []scan.File, int, error) {
	resolver := vcs.NewResolver()
	var ignoreOpts []ignore.Option
	switch {
	case set.noIgnore:
		ignoreOpts = append(ignoreOpts, ignore.WithDisabled())
	case len(set.ignoreFiles) > 0:
		ignoreOpts = append(ignoreOpts, ignore.WithFiles(set.ignoreFiles...))
	}
	matcher := ignore.New(resolver, ignoreOpts...)

	// A transient progress line reports files examined as the walk proceeds; the
	// walk's size is unknown up front, so it shows an open scanned= counter. It is
	// erased when scanning ends, so the caller's next log (the discovery counts)
	// supplants it.
	tracker := set.reporter.Track(set.scanLabel, field.Scanned, 0)
	defer tracker.Stop()

	scanOpts := []scan.Option{
		scan.WithWorkers(set.workers),
		scan.WithIgnore(ignoreFunc(matcher, set.configs)),
		scan.WithProgress(tracker.Set),
		scan.WithRequireDirective(set.requireDirective),
	}
	if set.maxSize > 0 {
		scanOpts = append(scanOpts, scan.WithMaxSize(set.maxSize))
	}
	files, scanned, err := scan.Scan(ctx, roots, scanOpts...)
	if err != nil {
		return resolver, files, scanned, err
	}
	// The walk visits every directory and file - including a repo carrying only
	// .git and a malformed .clover.yaml - so a bad config is seen even when no
	// directive file survives. excludedByConfig swallowed its load error to keep
	// scanning; surface it now as the hard error it is, before the version gate
	// (which would otherwise drop every file and exit 0).
	if err = set.configs.Err(); err != nil {
		return resolver, files, scanned, err
	}
	files, err = gateVersions(set.configs, set.current, files)
	return resolver, files, scanned, err
}

// ignoreFunc combines the ignore-file matcher with each repository's configured
// paths.exclude globs (resolved per root through configs): a path is skipped
// when either rejects it. A nil resolver applies no excludes.
func ignoreFunc(matcher *ignore.Matcher, configs *config.Resolver) scan.IgnoreFunc {
	return func(path string, isDir bool) bool {
		if excludedByConfig(configs, path, isDir) {
			return true
		}
		return matcher.Ignore(path, isDir)
	}
}

// excludedByConfig reports whether path matches a paths.exclude glob in the
// config governing its repository root. The globs are matched relative to that
// root, so a repo's "vendor/**" excludes its own vendored tree wherever the scan
// was launched from. A malformed config is treated as no excludes here and left
// for the version gate to surface.
func excludedByConfig(configs *config.Resolver, path string, isDir bool) bool {
	if configs == nil {
		return false
	}
	dir := path
	if !isDir {
		dir = filepath.Dir(path)
	}
	cfg, err := configs.ForDir(dir)
	if err != nil {
		return false
	}
	globs := cfg.ExcludeGlobs()
	if len(globs) == 0 {
		return false
	}
	rel := relTo(configs.Root(dir), path)
	for _, glob := range globs {
		if doublestar.ValidatePattern(glob) && doublestar.MatchUnvalidated(glob, rel) {
			return true
		}
	}
	return false
}

// relTo returns path relative to root in slash form, falling back to path's own
// slash form when it does not lie under root.
func relTo(root, path string) string {
	abs := path
	if a, err := filepath.Abs(path); err == nil {
		abs = a
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// gateVersions applies each repository's required-version gate: a repo whose
// required-version the running clover does not satisfy is skipped - its files
// dropped with a one-line warning - and the run proceeds for the rest. A
// malformed project config is a hard error instead, a bug to fix rather than a
// benign version mismatch. The gate is inert without a resolver or a parseable
// current version.
func gateVersions(
	configs *config.Resolver,
	current string,
	files []scan.File,
) ([]scan.File, error) {
	if configs == nil {
		return files, nil
	}
	blocked := set.New[string]()
	decided := set.New[string]()
	for _, f := range files {
		root := configs.Root(filepath.Dir(f.Path))
		if decided.Contains(root) {
			continue
		}
		decided.Add(root)
		cfg, err := configs.ForDir(filepath.Dir(f.Path))
		if err != nil {
			return nil, err
		}
		if verr := cfg.CheckVersion(current); verr != nil {
			blocked.Add(root)
			clog.Warn().
				Path(field.Path, root).
				Str(field.Required, requiredConstraint(cfg)).
				Str(field.Version, cversion.RemovePrefix(current)).
				Msg("Skipping repository - clover does not satisfy its `required-version`")
		}
	}
	if blocked.Len() == 0 {
		return files, nil
	}
	kept := make([]scan.File, 0, len(files))
	for _, f := range files {
		if !blocked.Contains(configs.Root(filepath.Dir(f.Path))) {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

// requiredConstraint returns the constraint a config requires, "" when unset.
func requiredConstraint(cfg *config.Config) string {
	if cfg == nil || cfg.RequiredVersion == nil {
		return ""
	}
	return *cfg.RequiredVersion
}

// build scans roots and binds the discovered directives into a plan ready for
// either resolution or validation.
func build(ctx context.Context, roots []string, opts ...Option) (*plan, []scan.File, int, error) {
	set := newSettings(opts...)
	resolver, files, scanned, err := scanRoots(ctx, roots, set)
	if err != nil {
		return nil, nil, 0, err
	}
	return newPlan(files, resolver, set), files, scanned, nil
}

// plan holds the state a run threads between seams: the flattened markers, each
// file's lines for rendering, the run-scoped registry follow markers read, the
// progress reporter, and one result slot per marker. Each task writes only its
// own slot, so the slice needs no lock - the same discipline the executor uses
// internally.
type plan struct {
	configs        *config.Resolver
	downgrade      *bool
	checksumSource *checksum.Resolver
	deep           *bool
	disabled       []Result
	force          *bool
	lines          map[string][]string
	markers        []Marker
	now            time.Time
	parseErrors    []Result
	prerelease     *bool
	registry       *registry.Registry
	reporter       progress.Reporter
	cooldown       *time.Duration
	results        []Result
	tasks          []progress.Task
	truncationSink func(provider.Truncation)
	verify         *bool
	workers        int
}

// newPlan flattens the scanned files into markers and pre-seeds a result per
// marker, namespacing ids by repository so the same id in two repositories does
// not collide.
func newPlan(files []scan.File, resolver *vcs.Resolver, set settings) *plan {
	lines := make(map[string][]string, len(files))
	var (
		markers     []Marker
		parseErrors []Result
		disabled    []Result
	)
	for _, f := range files {
		lines[f.Path] = f.Lines
		parseErrors = append(parseErrors, parseErrorResults(f)...)
		ms := Markers(f, resolver)
		if set.infer {
			// A written directive's target is governed; every other recognized
			// line earns a synthetic marker, resolved exactly like provider=auto.
			governed := make(map[int]bool, len(ms))
			for _, m := range ms {
				governed[m.Target] = true
			}
			ms = append(ms, InferredMarkers(f, governed)...)
		}
		for _, m := range ms {
			if !set.filter.Match(m.Tags) {
				continue
			}
			off, reason, err := disabledState(m.Directive)
			switch {
			case err != nil:
				parseErrors = append(
					parseErrors,
					Result{Marker: m, NewLine: targetLine(lines, m), Err: err},
				)
			case off:
				disabled = append(
					disabled,
					Result{
						Marker:   m,
						NewLine:  targetLine(lines, m),
						Disabled: true,
						Reason:   reason,
					},
				)
			default:
				markers = append(markers, m)
			}
		}
	}

	results := make([]Result, len(markers))
	for i, m := range markers {
		results[i] = Result{Marker: m, NewLine: targetLine(lines, m)}
	}

	checksumClient := httpcache.New()
	return &plan{
		configs:        set.configs,
		downgrade:      set.downgrade,
		checksumSource: checksum.NewResolver(checksumClient),
		deep:           set.deep,
		disabled:       disabled,
		force:          set.force,
		lines:          lines,
		markers:        markers,
		now:            set.now,
		parseErrors:    parseErrors,
		prerelease:     set.prerelease,
		registry:       registry.New(),
		reporter:       set.reporter,
		cooldown:       set.cooldown,
		results:        results,
		truncationSink: set.truncationSink,
		verify:         set.verify,
		workers:        set.workers,
	}
}

// disabledState interprets a directive's disabled key: disabled=false (or absent)
// leaves the marker enabled; disabled=true disables it with no reason; any other
// non-empty value disables it and is the reason reported. An empty disabled value
// is malformed - directives never carry an empty value.
func disabledState(d directive.Directive) (bool, string, error) {
	v, ok := d.Get(constant.DirectiveDisabled)
	switch {
	case !ok, v == constant.BoolFalse:
		return false, "", nil
	case v == constant.BoolTrue:
		return true, "", nil
	case v == "":
		return false, "", fmt.Errorf(
			"%q needs %s, %s, or a reason",
			constant.DirectiveDisabled,
			constant.BoolTrue,
			constant.BoolFalse,
		)
	default:
		return true, v, nil
	}
}

func parseErrorResults(f scan.File) []Result {
	results := make([]Result, 0, len(f.Errors))
	for _, e := range f.Errors {
		r := Result{
			Marker:  Marker{File: f.Path, Line: e.Line, Target: e.Line, Sidecar: e.Sidecar},
			NewLine: lineAt(f.Lines, e.Line),
		}
		if e.Skip {
			r.Skipped, r.Reason = true, e.Err.Error()
		} else {
			r.Err = e.Err
		}
		results = append(results, r)
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
		case r.Err != nil && errors.Is(r.Err, directive.ErrUnknownKey):
			// An unknown key is a config-hygiene problem lint rejects, but run
			// stays resilient: warn, leave the line untouched, and carry on with
			// the other markers rather than failing the whole run.
			p.results[i].Skipped = true
			p.results[i].Reason = r.Err.Error()
			p.tasks[i].Skip(r.Err.Error())
		case r.Err != nil && errors.Is(r.Err, errCooldownUnsupported):
			// The source cannot honor the cooldown, so hold the line and warn
			// rather than fail: this is a source limitation, not a run error.
			p.results[i].Skipped = true
			p.results[i].Reason = r.Err.Error()
			p.tasks[i].Skip(r.Err.Error())
		case r.Err != nil:
			p.results[i].Err = r.Err
		}
	}
}

// producer returns the closure that resolves marker i from its upstream
// provider, reporting the outcome to the marker's progress task.
func (p *plan) producer(i int) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx = provider.WithDeep(ctx, p.deepFor(p.markers[i]))
		p.tasks[i].Update("resolving")
		err := p.resolveProducer(ctx, i)
		p.report(i, err)
		return err
	}
}

// configFor returns the config governing marker file's repository root, nil-safe.
// A load error degrades to no config here; the version gate surfaces a malformed
// config authoritatively before any marker resolves.
func (p *plan) configFor(file string) *config.Config {
	if p.configs == nil {
		return nil
	}
	cfg, _ := p.configs.ForDir(filepath.Dir(file))
	return cfg
}

// deepFor reports whether marker m does a deep lookup: the --deep override, else
// its root's run.deep, and always on when verify is in force (verification needs
// the complete history).
func (p *plan) deepFor(m Marker) bool {
	cfg := p.configFor(m.File)
	verify := cmp.Or(p.verify, cfg.Verify())
	return ptr.Deref(cmp.Or(p.deep, cfg.Deep())) || ptr.Deref(verify)
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
			"could not infer a provider for the target line; set %q explicitly",
			constant.DirectiveProvider,
		)
	}
	return nil, fmt.Errorf("unknown provider %q", name)
}

func (p *plan) resolveProducer(ctx context.Context, i int) error {
	m := p.markers[i]

	if err := checkKeys(m); err != nil {
		return err
	}

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

	// A line-anchored provider (manual) resolves to the value already on the
	// line, skipping discovery and selection entirely.
	if _, ok := prov.(provider.Anchorer); ok {
		return p.anchor(i, m, line, located)
	}

	// A recency-ordered provider lists newest-first, so a truncated shallow lookup
	// only matters when selection finds nothing. Capture truncation per-marker for
	// the gated --deep hint instead of letting it feed the run-wide blanket hint.
	var truncated atomic.Bool
	if _, ok := prov.(provider.RecencyOrderer); ok {
		ctx = provider.WithTruncationSink(ctx, func(provider.Truncation) { truncated.Store(true) })
	}

	// Selection is pinned to the located tag's own suffix (see variantInclude),
	// so tell the provider, which may then skip tags that could never match.
	if q := qualifierHint(located.Current(), m.Directive); q != "" {
		ctx = provider.WithQualifier(ctx, q)
	}
	// Likewise a tag without the marker's tag-prefix can never be selected (see
	// prefixedAttrs), so the provider may skip those too.
	prefix, _ := m.Directive.Get(constant.RuleTagPrefix)
	if prefix != "" {
		ctx = provider.WithTagPrefix(ctx, prefix)
	}
	// With downgrades off, selection cannot pick below the current version, so a
	// version-ordered provider may stop paging once its listing passes it. The
	// gate mirrors the downgrade precedence below: a CLI/config override
	// decides, and otherwise any directive-level rule is assumed live. A
	// tag-prefix suppresses the floor - the provider sees raw tags whose
	// version-ordering the prefix may break.
	cfg := p.configFor(m.File)
	downgrade := cmp.Or(p.downgrade, cfg.Downgrade())
	downgradeOff := downgrade != nil && !*downgrade ||
		downgrade == nil && !m.Directive.Has(constant.RuleDowngrade)
	if located.Semver() != nil && prefix == "" && downgradeOff {
		ctx = provider.WithVersionFloor(ctx, located.Semver().String())
	}

	candidates, err := prov.Discover(ctx, resource)
	if err != nil {
		return err
	}
	p.results[i].Truncated = truncated.Load()

	// A cooldown needs a publication date to measure age. When one is in force
	// but the source dates nothing it returned, honoring it is impossible, so
	// clover skips the marker with a warning instead of silently updating past a
	// cooldown that cannot apply.
	if cd := p.effectiveCooldown(m, cfg); cd > 0 && !p.now.IsZero() && !anyDated(candidates) {
		return errCooldownUnsupported
	}

	opts, err := rule.Compile(m.Directive, located.Semver())
	if err != nil {
		return err
	}
	if opt, ok := variantInclude(located.Current(), m.Directive); ok {
		opts = append(opts, opt)
	}
	// Exempt the line's own suffix from the prerelease gate, so a vendor track
	// (1.15.0-ent) the include above already scoped to stays selectable.
	opts = append(opts, version.WithQualifier(version.Qualifier(located.Current())))
	opts = append(opts, version.WithNow(p.now))
	// A CLI override wins over the root's config default, which wins over the
	// directive's own rule; appended after the directive options so a set value
	// takes precedence. nil at both levels leaves the per-directive rule in force.
	if downgrade != nil {
		opts = append(opts, version.WithDowngrade(*downgrade))
	}
	if prerelease := cmp.Or(p.prerelease, cfg.Prerelease()); prerelease != nil {
		opts = append(opts, version.WithPrerelease(*prerelease))
	}
	if d, ok := p.cooldownFor(m, cfg); ok {
		opts = append(opts, version.WithCooldown(d))
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
	if prefix != "" {
		extract = prefixedAttrs(prefix)
	}

	chosen, reason, ok := version.SelectReason(located.Semver(), candidates, extract, opts...)
	if !ok {
		if detail := reason.Detail(); detail != "" {
			return fmt.Errorf("%w: %s", ErrNoCandidate, detail)
		}
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
		if r, ok := located.(match.Renderer); ok {
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
	located match.Location,
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
	if d, ok := p.cooldownFor(m, p.configFor(m.File)); ok {
		cooldown = d
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

// cooldownFor resolves the cooldown override chain for a marker: an explicit
// CLI --cooldown replaces even a directive's own value (zero disables), and the
// config default fills in only when the directive is silent - a written
// cooldown is a deliberate per-line choice, so unlike the boolean defaults the
// config never overrides it. ok is false when neither source applies and the
// directive's own rule (already compiled) stands.
func (p *plan) cooldownFor(m Marker, cfg *config.Config) (time.Duration, bool) {
	if p.cooldown != nil {
		return *p.cooldown, true
	}
	if !m.Directive.Has(constant.RuleCooldown) {
		if d := cfg.Cooldown(); d > 0 {
			return d, true
		}
	}
	return 0, false
}

// effectiveCooldown resolves the cooldown that will govern m after the override
// chain: a --cooldown flag or run.cooldown default (via cooldownFor) wins,
// otherwise the directive's own cooldown stands. It is what the unsupported-
// source skip check consults, matching the value selection ends up applying.
func (p *plan) effectiveCooldown(m Marker, cfg *config.Config) time.Duration {
	if d, ok := p.cooldownFor(m, cfg); ok {
		return d
	}
	d, _ := m.Directive.Duration(constant.RuleCooldown)
	return d
}

// anyDated reports whether any candidate carries a publication date, the signal
// that the source can measure a cooldown at all.
func anyDated(candidates []model.Candidate) bool {
	return slices.ContainsFunc(candidates, func(c model.Candidate) bool {
		return !c.PublishedAt.IsZero()
	})
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
	located match.Location,
	chosen model.Candidate,
) error {
	if m.ID != "" {
		old := model.Candidate{Version: located.Current(), Semver: located.Semver()}
		p.registry.Set(m.ID, registry.Entry{Old: old, New: chosen})
	}
	if err := p.render(i, line, located, chosen); err != nil {
		return err
	}
	if linker, ok := prov.(provider.Linker); ok {
		p.results[i].ResolvedURL = linker.URL(resource, chosen)
		p.results[i].CurrentURL = linker.URL(resource, currentCandidate(located, chosen))
	}
	p.results[i].Verify = verifyPin(located, chosen)
	if p.results[i].Verify == nil && p.deepVerify(m) {
		p.results[i].Verify = p.verifyBranch(ctx, prov, resource, located, chosen, m)
	}
	return nil
}

// currentCandidate builds a candidate for the version currently on the line so a
// linker can resolve its upstream page. The current value carries no upstream
// ref of its own, so it is inferred from the resolved ref's prefix - e.g. a
// resolved ref of "v7.0.0" yields prefix "v" and a current ref of "v6.0.3". It
// is empty when the current value is not a parseable version, leaving the from
// value unlinked.
func currentCandidate(located match.Location, chosen model.Candidate) model.Candidate {
	if located.Semver() == nil {
		return model.Candidate{}
	}
	cur := located.Current()
	prefix := strings.TrimSuffix(chosen.Ref, cversion.RemovePrefix(chosen.Ref))
	return model.Candidate{
		Version: cur,
		Semver:  located.Semver(),
		Ref:     prefix + cversion.RemovePrefix(cur),
	}
}

// anchor resolves a line-anchored provider (manual): the value is whatever the
// target line already carries, published under the marker's id so followers and
// side values can track it. The line is never rewritten - a person owns the
// value - so this skips discovery, selection, digest resolution, render, and pin
// verification, and reports the marker as up to date.
func (p *plan) anchor(i int, m Marker, line string, located match.Location) error {
	current := model.Candidate{Version: located.Current(), Semver: located.Semver()}
	if m.ID != "" {
		p.registry.Set(m.ID, registry.Entry{Old: current, New: current})
	}
	p.results[i].Current = located.Current()
	p.results[i].Resolved = located.Current()
	p.results[i].Written = located.Current()
	p.results[i].NewLine = line
	p.results[i].Changed = false
	return nil
}

// deepVerify reports whether the deep tag-on-branch check runs for marker m: the
// --verify override wins, then the root's run.verify default, else the marker's
// own verify=/verify-branch=.
func (p *plan) deepVerify(m Marker) bool {
	if verify := cmp.Or(p.verify, p.configFor(m.File).Verify()); verify != nil {
		return *verify
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
	located match.Location,
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
		return nil, fmt.Errorf(
			"invalid %q pattern %q: %w",
			constant.DirectiveVerifyBranch,
			raw,
			err,
		)
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
		return nil, fmt.Errorf(
			"no branch matches the %q pattern %q",
			constant.DirectiveVerifyBranch,
			raw,
		)
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
func verifyPin(located match.Location, chosen model.Candidate) error {
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

	if err := checkKeys(m); err != nil {
		return err
	}

	// A track= marker carries no provider, so it lands here misclassified as a
	// follower; its preconditions reject it with the actionable error.
	if m.Directive.Has(constant.DirectiveTrack) {
		if err := trackPreconditions(m); err != nil {
			return err
		}
	}

	// Locate first so the held-digest guard can read the value already on the
	// line before deciding whether a network fetch is even warranted.
	line, located, err := p.locate(m)
	if err != nil {
		return err
	}

	// Security guard: a followed digest is held in place while the version it
	// follows is unchanged, so a re-published artifact can never move a pin on its
	// own. --force (or run.force) re-pins deliberately.
	if p.heldDigest(m, located) && !p.forceFor(m) {
		return p.holdFollower(i, m, line, located)
	}

	resolved, err := p.followValue(ctx, m)
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

// heldDigest reports whether follower m resolves a digest (sha256 or commit)
// that must not move: its value kind is a cryptographic pin, the version it
// follows is unchanged, and a digest is already on the line. An anchored
// (manual) producer is exempt - its Old==New holds even across a human's edit,
// so the guard would otherwise freeze a deliberate manual version bump.
func (p *plan) heldDigest(m Marker, located match.Location) bool {
	if m.Value != constant.ValueSha256 && m.Value != constant.ValueCommit {
		return false
	}
	// An empty or all-zero line carries no pin to protect (the conventional
	// 000... placeholder of an unbootstrapped digest): let it populate.
	if unpinnedDigest(located.Current()) {
		return false
	}
	if p.producerAnchored(m.From) {
		return false
	}
	entry, ok := p.registry.Get(m.From)
	return ok && entry.Old.Version != "" &&
		cversion.EqualString(entry.Old.Version, entry.New.Version)
}

// holdFollower records follower m as up to date without re-fetching: the line
// keeps its current digest, the producer entry is republished so any chain stays
// consistent, and the result reports the held value as a clean no-op.
func (p *plan) holdFollower(i int, m Marker, line string, located match.Location) error {
	if m.ID != "" {
		if entry, ok := p.registry.Get(m.From); ok {
			p.registry.Set(m.ID, entry)
		}
	}
	current := located.Current()
	p.results[i].Current = current
	p.results[i].Resolved = current
	p.results[i].Written = current
	p.results[i].NewLine = line
	p.results[i].Changed = false
	p.results[i].Moved = p.movedCommit(m, current)
	clog.Debug().
		Str(field.Version, current).
		Msg("Held digest - version unchanged, pass `--force` to re-pin")
	return nil
}

// movedCommit reports the upstream commit a held commit pin's tag moved to, or
// "" when it is unchanged. A force-moved tag is unexpected for a pinned semver
// version (the supply-chain signal worth warning on), but expected for a
// floating producer (track=), which is exempted. The producer already peeled the
// tag to its commit during discovery, so the comparison costs no extra fetch.
func (p *plan) movedCommit(m Marker, current string) string {
	if m.Value != constant.ValueCommit {
		return ""
	}
	if p.producerFloating(m.From) {
		return ""
	}
	entry, ok := p.registry.Get(m.From)
	if !ok || entry.New.Commit == "" || entry.New.Commit == current {
		return ""
	}
	return entry.New.Commit
}

// producerFloating reports whether the producer named by from tracks a floating
// ref (track=). Such a producer's target moves by design, so a held follower of
// it must not warn when the commit changes - the move is expected.
func (p *plan) producerFloating(from string) bool {
	m, ok := p.markerByID(from)
	return ok && m.Directive.Has(constant.DirectiveTrack)
}

// unpinnedDigest reports whether s is an empty or all-zero digest - the
// conventional placeholder for a value not yet pinned, which carries nothing to
// protect and so always populates.
func unpinnedDigest(s string) bool {
	return s == "" || strings.Trim(s, "0") == ""
}

// producerAnchored reports whether the producer named by from is a line-anchored
// (manual) provider. Such a producer publishes Old==New unconditionally, so the
// held-digest guard must skip it or a manual version bump would never re-pin.
func (p *plan) producerAnchored(from string) bool {
	m, ok := p.markerByID(from)
	return ok && m.Provider == constant.ProviderManual
}

// markerByID finds the marker whose follow-edge ID matches id.
func (p *plan) markerByID(id string) (Marker, bool) {
	for _, m := range p.markers {
		if m.ID == id {
			return m, true
		}
	}
	return Marker{}, false
}

// forceFor reports whether followed digests are re-pinned for marker m even when
// their version is unchanged: the --force override wins, else the root's
// run.force. The default holds an unchanged version's digest.
func (p *plan) forceFor(m Marker) bool {
	return ptr.Deref(cmp.Or(p.force, p.configFor(m.File).Force()))
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
// checksum file (tier one: an explicit sha256-url, templated with <version>).
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
	return p.checksumSource.Resolve(ctx, checksum.Request{
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
func (p *plan) locate(m Marker) (string, match.Location, error) {
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
			return nil, fmt.Errorf(
				"%q is not supported for provider %q",
				constant.DirectiveTrack,
				m.Provider,
			)
		}
	}

	find, hasFind := m.Directive.Get(constant.DirectiveFind)
	replace, hasReplace := m.Directive.Get(constant.DirectiveReplace)
	if !hasFind && hasReplace {
		return nil, fmt.Errorf(
			"%q needs %q",
			constant.DirectiveReplace,
			constant.DirectiveFind,
		)
	}
	if m.Sidecar && m.Provider == constant.ProviderDocker &&
		strings.Contains(line, constant.DockerDigestMarker) {
		pin := match.NewDockerPin()
		if hasFind && !hasReplace {
			return match.NewGuarded(find, pin)
		}
		if !hasFind {
			return pin, nil
		}
	}
	if !hasFind {
		if m.Sidecar && m.Provider == constant.ProviderDocker {
			return match.NewDockerTag(), nil
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
	located match.Location,
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
func renderedValue(located match.Location, candidate model.Candidate) string {
	if r, ok := located.(match.Renderer); ok {
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
	for _, r := range p.disabled {
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
	names := make([]string, len(c.Assets))
	for i, a := range c.Assets {
		names[i] = a.Name
	}
	return version.Attrs{
		Tag:         c.Version,
		Semver:      c.Semver,
		Prerelease:  c.Prerelease,
		PublishedAt: c.PublishedAt,
		Assets:      names,
	}
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
		return version.Attrs{
			Tag:         rest,
			Semver:      semver,
			Prerelease:  c.Prerelease,
			PublishedAt: c.PublishedAt,
		}
	}
}

// qualifierHint returns the qualifier selection will be pinned to: the located
// tag's suffix when variantInclude's scoping applies, "" when an explicit
// directive include overrides it or the tag is plain.
func qualifierHint(raw string, d directive.Directive) string {
	if d.Has(constant.RuleInclude) {
		return ""
	}
	return version.Qualifier(raw)
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
	if want := version.Qualifier(raw); want != "" {
		// A qualified line (1.27-alpine, 1.15.0-ent) keeps only its own suffix,
		// recognized variant or not, so a vendor track stays on that track.
		return version.WithInclude(func(tag string) bool {
			return version.Qualifier(tag) == want
		}), true
	}
	// A plain line keeps only plain tags: a recognized variant is dropped here,
	// while a genuine prerelease still reaches the prerelease gate so the failure
	// names prerelease rather than the filter.
	return version.WithInclude(func(tag string) bool {
		_, got := version.SplitVariant(tag)
		return got == ""
	}), true
}

// label is the progress display name for a marker: its file and directive line,
// where the user will see the change.
func label(m Marker) string {
	return fmt.Sprintf("%s:%d", filepath.Base(m.File), m.Line+1)
}
