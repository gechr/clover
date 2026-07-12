package mode

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/gechr/clover/internal/comment"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/infer"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/progress"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/gechr/x/set"
	xslices "github.com/gechr/x/slices"
	xsync "github.com/gechr/x/sync"
)

// syntheticInferencePath is the YAML path annotate hands [match.Infer] when
// recognizing a JSON leaf: the auto-routes that read an image:/uses: reference
// are scoped to YAML, so a synthesized `<key>: <value>` line is inferred as if it
// were YAML. Recognition reuses the real inference engine without teaching it
// JSON's quoted-key syntax; resolution still runs against the real JSON path.
const syntheticInferencePath = "clover-sidecar.yaml"

// versionPlaceholder is the find-pattern token the rewriter rewrites in place; a
// generated docker entry anchors its find on `<repository>:<version>`.
const versionPlaceholder = "<version>"

func sidecarModeline() string {
	return "# yaml-language-server: $schema=" + sidecar.SchemaURL() + "\n\n"
}

// strictJSON reports whether path is a strict-JSON file - one that cannot host an
// inline comment, so a directive for it must live in a sidecar. The comment-
// hosting JSON dialects (.jsonc, .json5) are excluded: they take inline comments
// like any other commentable format.
func strictJSON(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

// sidecarTarget reports whether path is a comment-less target - one whose
// directives must live in a sidecar because an inline comment would corrupt it.
func sidecarTarget(path string) bool {
	return strictJSON(path) || match.PythonVersionFile(path)
}

// proposeSidecar builds the sidecar proposal for a comment-less target: strict
// JSON gets the leaf-based generator, a pyenv .python-version file the
// line-based one. It must only be called for a [sidecarTarget]. Only the JSON
// generator takes force - a text entry carries no source keys beyond the
// provider, so there is no drift to repair.
func proposeSidecar(file scan.File, force bool) (*AnnotateSidecar, []AnnotateSkip) {
	if strictJSON(file.Path) {
		return annotateSidecar(file, force)
	}
	return annotateTextSidecar(file)
}

// AnnotateChange is one comment line annotate would write: At is the 0-based
// line index it acts on, Line the comment text, Provider the provider the line
// was inferred to track (for reporting). Existing distinguishes the two shapes -
// a fresh insertion above a recognized line (Existing false), or a rewrite of an
// existing directive comment at At into its canonical minimal form (Existing
// true, only under --force).
type AnnotateChange struct {
	At       int
	Line     string
	Provider string
	Existing bool
}

// AnnotateSkip records a recognized annotation candidate that annotate left
// alone, with the reason it failed an opt-out or offline safety gate. Skips are
// diagnostics only; they never affect Added/Updated counts.
type AnnotateSkip struct {
	Line    int // 0-based target line; -1 when the reason belongs to the file
	Reason  string
	Sidecar string
}

// AnnotateFile is the annotate outcome for one file: the comment lines it would
// add or rewrite (for a commentable file), or the sidecar it would generate (for
// a comment-less strict-JSON target), and whether they were written. The two are
// mutually exclusive - a file either hosts inline comments or it does not.
type AnnotateFile struct {
	Path     string
	Changes  []AnnotateChange
	Sidecar  *AnnotateSidecar
	Skips    []AnnotateSkip
	Written  bool
	WriteErr error
}

// AnnotateSidecar is the sidecar annotate would write beside a comment-less
// target: its path, the full canonical YAML to write, and one entry-change per
// fresh or rewritten entry (for counts and reporting). Content is the exact bytes
// `--write` lays down - a fresh document, or the existing one with new entries
// appended.
type AnnotateSidecar struct {
	Path    string
	Content string
	Entries []SidecarEntryChange
}

// SidecarEntryChange is one sidecar entry annotate would add or, under --force,
// rewrite: the 0-based target line in the target file it governs, the provider
// the entry tracks (for reporting), and whether it replaces an existing entry
// rather than introducing a new one.
type SidecarEntryChange struct {
	Target   int
	Provider string
	Existing bool
}

// AnnotateSummary is the annotate outcome over all roots, in file order.
type AnnotateSummary struct {
	Files   []AnnotateFile
	Scanned int // total files examined during the walk
}

// Added reports how many fresh annotations annotate would insert.
func (s AnnotateSummary) Added() int { return s.count(false) }

// Updated reports how many existing annotations annotate would rewrite (--force).
func (s AnnotateSummary) Updated() int { return s.count(true) }

// Total reports the total number of comment lines annotate would add or rewrite.
func (s AnnotateSummary) Total() int { return s.Added() + s.Updated() }

// OK reports whether every recognized line is already annotated canonically -
// nothing to add or rewrite.
func (s AnnotateSummary) OK() bool { return s.Total() == 0 }

func (s AnnotateSummary) count(existing bool) int {
	var n int
	for _, f := range s.Files {
		for _, c := range f.Changes {
			if c.Existing == existing {
				n++
			}
		}
		if f.Sidecar != nil {
			for _, e := range f.Sidecar.Entries {
				if e.Existing == existing {
					n++
				}
			}
		}
	}
	return n
}

// Annotate scans every text file under roots and, for each line that clover can
// already track but is not yet annotated, proposes a `@clover`
// comment above it - the inverse of the auto-detection that resolves such a
// marker. It is the onboarding command: it leans on the same dispatch routes
// [match.Infer] uses, so a line only earns an annotation that is guaranteed to
// re-resolve. By default it never touches an existing annotation; with force it
// also rewrites a recognized line's directive into its canonical minimal form,
// collapsing keys inference supplies while preserving every rule key. With write
// off it reports what it would do and writes nothing; otherwise it rewrites each
// changed file atomically. With sidecars off it proposes inline comments only,
// leaving comment-less targets untouched instead of generating sidecars.
func Annotate(
	ctx context.Context,
	roots []string,
	write bool,
	force bool,
	sidecars bool,
	configs *config.Resolver,
	reporter progress.Reporter,
	parallelism int,
	opts ...pipeline.Option,
) (AnnotateSummary, error) {
	opts = append(opts,
		pipeline.WithConfig(configs),
		pipeline.WithRequireDirective(false),
	)
	files, scanned, err := pipeline.Scan(ctx, roots, opts...)
	if err != nil {
		return AnnotateSummary{}, err
	}

	// Inferring and verifying each file's trackable lines is the slow half on a
	// large tree; a transient line keeps it from looking hung. Its size is known,
	// so it shows a fraction; it is erased on return, so the discovery log
	// supplants it.
	verify := reporter.Track("Verifying annotation candidates", field.Progress, len(files))
	defer verify.Stop()

	// Each file is annotated independently - the per-file work only reads immutable
	// shared state and writes its own file - so files are processed concurrently,
	// each result kept at its own index to preserve order. A skipped sidecar file
	// leaves a nil slot, compacted away below.
	results := make([]*AnnotateFile, len(files))
	var done atomic.Int64
	xsync.Parallel(parallelism, len(files), func(i int) {
		// Count each file as it finishes - the closure defers the increment to exit
		// (not defer-time), so the final tally reaches len(files); registered before
		// the sidecar early-return so skipped files still count.
		defer func() { verify.Set(int(done.Add(1))) }()
		file := files[i]
		if scan.IsSidecar(file.Path) {
			return // never propose inline directives inside a sidecar file
		}
		// A comment-less target cannot host an inline comment, so a recognized
		// line earns a sidecar entry instead of a comment that would corrupt it.
		// With sidecars off the target is left untouched - it must not fall
		// through to the inline path either.
		if sidecarTarget(file.Path) {
			if sidecars {
				results[i] = annotateSidecarFile(file, force, write)
			}
			return
		}
		annotated := annotateFile(file, force)
		if len(annotated.Changes) > 0 && write {
			lines := applyAnnotations(file.Lines, annotated.Changes)
			if err := writeFile(file.Path, lines); err != nil {
				annotated.WriteErr = err
			} else {
				annotated.Written = true
			}
		}
		results[i] = &annotated
	})

	out := make([]AnnotateFile, 0, len(files))
	for _, annotated := range results {
		if annotated != nil {
			out = append(out, *annotated)
		}
	}
	return AnnotateSummary{Files: out, Scanned: scanned}, nil
}

// annotateSidecarFile proposes the sidecar for a comment-less target and, with
// write, lays it down.
func annotateSidecarFile(file scan.File, force, write bool) *AnnotateFile {
	generated, skips := proposeSidecar(file, force)
	annotated := AnnotateFile{Path: file.Path, Sidecar: generated, Skips: skips}
	if annotated.Sidecar != nil && write {
		if err := writeSidecar(annotated.Sidecar); err != nil {
			annotated.WriteErr = err
		} else {
			annotated.Written = true
		}
	}
	return &annotated
}

// annotateFile walks a file's lines and collects the annotations to add (and,
// under force, the existing ones to canonicalize). A line bound to an existing
// directive is left alone unless force is set; a line clover does not recognize
// is never annotated, so manual, http, follow, and unrelated lines are untouched.
func annotateFile(file scan.File, force bool) AnnotateFile {
	syntax := comment.For(file.Path)
	annotated := AnnotateFile{Path: file.Path}

	// A directive binds to the line below it, so its comment and target are both
	// off-limits to a fresh insertion. existing maps a target line to its
	// directive so force can rewrite the comment above it.
	governed := set.New[int]()
	existing := map[int]scan.Located{}
	for _, loc := range file.Found {
		if loc.Sidecar {
			governed.Add(loc.Line) // the sidecar already rewrites this line; never re-annotate it
			continue
		}
		governed.Add(loc.Line, loc.Line+1)
		existing[loc.Line+1] = loc
	}

	for i, line := range file.Lines {
		if file.Ignored.Contains(i) {
			if infer.Recognizable(file.Path, file.Lines, i) {
				annotated.Skips = append(annotated.Skips, skip(i, "ignored"))
			}
			continue // a clover:ignore control opts this line out
		}
		// A commented-out example (# - uses: …, # image: …) is documentation, not a
		// live field, so it is never a target - inference reads raw line text and
		// would otherwise match inside the comment.
		if syntax.IsComment(line) {
			if infer.Recognizable(file.Path, file.Lines, i) {
				annotated.Skips = append(annotated.Skips, skip(i, "commented out"))
			}
			continue
		}
		// Recognize both matches the auto routes and runs the same offline
		// resolution checks lint does, so annotate never emits a directive lint
		// would reject: an incomplete reference, a resource that does not build,
		// or a version the rewriter cannot locate all skip with the reason.
		inf, reason, ok := infer.Recognize(file.Path, file.Lines, i)
		if !ok {
			continue
		}
		if reason != "" {
			annotated.Skips = append(annotated.Skips, skip(i, reason))
			continue
		}

		if loc, bound := existing[i]; bound {
			// An existing annotation is only canonicalized under force, and only when
			// it is one clover owns: provider=auto or a redundant explicit provider the
			// line itself infers. A deliberate non-inferable directive (provider=http,
			// a find/replace, a tracked ref) is left untouched - rewriting it to
			// provider=auto would drop keys it needs and break the marker.
			if !force || !forceEligible(loc.Directive, inf.Provider) {
				continue
			}
			if change, ok := rewrite(syntax, file.Lines[loc.Line], loc, inf); ok {
				annotated.Changes = append(annotated.Changes, change)
			}
			continue
		}
		if governed.Contains(i) {
			continue
		}
		if change, ok := insert(syntax, i, line, inf.Provider); ok {
			annotated.Changes = append(annotated.Changes, change)
		} else {
			annotated.Skips = append(annotated.Skips, skip(i, "comment syntax unavailable"))
		}
	}
	return annotated
}

// skip builds a target-line diagnostic.
func skip(line int, reason string) AnnotateSkip {
	return AnnotateSkip{Line: line, Reason: reason}
}

// forceEligible reports whether an existing directive may be canonicalized under
// --force. Only an annotation clover would itself produce qualifies: provider=auto
// or an explicit provider equal to what the line infers, and never one carrying
// find/replace or track, which select a different rewriter and are deliberate
// choices a collapse to provider=auto would silently break.
func forceEligible(d directive.Directive, inferred string) bool {
	if d.Has(constant.DirectiveFind) ||
		d.Has(constant.DirectiveReplace) ||
		d.Has(constant.DirectiveTrack) {
		return false
	}
	p, _ := d.Get(constant.DirectiveProvider)
	return p == constant.ProviderAuto || p == inferred
}

// insert builds the fresh annotation for a recognized, unannotated line: a
// `@clover` comment indented to match the line. ok is false when the file's
// syntax exposes no comment delimiter.
func insert(syntax comment.Syntax, i int, line, provider string) (AnnotateChange, bool) {
	body := directive.Render(canonicalDirective(directive.Directive{}, match.Inference{}))
	comment, ok := syntax.Comment(leadingWhitespace(line), body)
	if !ok {
		return AnnotateChange{}, false
	}
	return AnnotateChange{At: i, Line: comment, Provider: provider}, true
}

// rewrite canonicalizes an existing directive comment into its minimal form,
// returning the rewritten line. ok is false when the comment cannot be
// re-rendered or the canonical form is identical to what is already there.
func rewrite(
	syntax comment.Syntax,
	line string,
	loc scan.Located,
	inf match.Inference,
) (AnnotateChange, bool) {
	body := directive.Render(canonicalDirective(loc.Directive, inf))
	rendered, ok := syntax.Render(line, body)
	if !ok || rendered == line {
		return AnnotateChange{}, false
	}
	return AnnotateChange{
		At:       loc.Line,
		Line:     rendered,
		Provider: inf.Provider,
		Existing: true,
	}, true
}

// canonicalDirective returns the minimal directive annotate writes for a
// recognized line: provider=auto first, then every key from the existing
// directive except the ones auto-detection supplies from the line itself (see
// [inferenceOwns]), kept in their original order. For a fresh insertion the
// input is empty, so the result is just provider=auto. Dropping the inference-
// owned keys is what lets force both shed a redundant explicit value and repair
// one that has drifted from its line, while every rule key survives.
func canonicalDirective(d directive.Directive, inf match.Inference) directive.Directive {
	pairs := []directive.KV{{Key: constant.DirectiveProvider, Value: constant.ProviderAuto}}
	for _, kv := range d.Pairs {
		if inferenceOwns(kv.Key, inf) {
			continue
		}
		pairs = append(pairs, kv)
	}
	return directive.Directive{Pairs: pairs}
}

// inferenceOwns reports whether auto-detection supplies key for a line inf was
// inferred from, so force can drop it and let resolution re-derive it. The
// provider, repository, and registry are always inference's to supply; the host
// and product only when the line itself names one, so an explicit host pointing
// a reference at a self-managed instance survives the collapse.
func inferenceOwns(key string, inf match.Inference) bool {
	switch key {
	case constant.DirectiveProvider, constant.DirectiveRepository, constant.DirectiveRegistry:
		return true
	case constant.DirectiveHost:
		return inf.Host != ""
	case constant.DirectiveProduct:
		return inf.Product != ""
	case constant.DirectiveSource:
		return inf.Source != ""
	case constant.RuleTagPrefix:
		return inf.TagPrefix != ""
	default:
		return false
	}
}

// applyAnnotations returns a copy of lines with each change applied. Insertions
// shift every line below them, so changes are applied highest index first: each
// splice then leaves the lower indices it has not reached still valid.
func applyAnnotations(lines []string, changes []AnnotateChange) []string {
	out := make([]string, len(lines))
	copy(out, lines)

	ordered := slices.Clone(changes)
	slices.SortFunc(ordered, func(a, b AnnotateChange) int { return cmp.Compare(b.At, a.At) })
	for _, c := range ordered {
		if c.Existing {
			out[c.At] = c.Line
			continue
		}
		out = slices.Insert(out, c.At, c.Line)
	}
	return out
}

// leadingWhitespace returns the run of spaces and tabs that indents line, so a
// synthesized comment aligns with the line it annotates.
func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// sidecarEntry is one entry annotate would render into a sidecar: the directive
// and the 0-based target line it governs (used for reporting).
type sidecarEntry struct {
	directive directive.Directive
	target    int
}

// annotateSidecar proposes the sidecar for a comment-less JSON target. It
// enumerates the JSON's string leaves, keeps the ones that name a trackable
// reference and are not already governed, and renders each as an explicit
// directive (provider + repository, a jq locator, and a repository-anchored find
// so a later edit of the line fails loud rather than tracking the wrong source).
// Without force it appends only fresh entries, leaving an existing sidecar's bytes
// untouched; with force it re-derives the source keys of every reproducible entry
// too, repairing one that has drifted - preserving every other entry, locator, and
// comment. It returns nil when there is nothing to add or rewrite.
func annotateSidecar(file scan.File, force bool) (*AnnotateSidecar, []AnnotateSkip) {
	source := []byte(strings.Join(file.Lines, "\n"))
	leaves, err := sidecar.Leaves(source)
	if err != nil {
		return nil, []AnnotateSkip{{Line: -1, Reason: err.Error()}}
	}

	governed := set.New[int]()
	for _, loc := range file.Found {
		if loc.Sidecar {
			governed.Add(loc.Line)
		}
	}

	var fresh []sidecarEntry
	var skips []AnnotateSkip
	for _, leaf := range leaves {
		if leaf.Line >= len(file.Lines) {
			continue
		}
		if file.Ignored.Contains(leaf.Line) {
			if recognizedLeaf(leaf) {
				skips = append(skips, skip(leaf.Line, "ignored"))
			}
			continue
		}
		if governed.Contains(leaf.Line) {
			continue
		}
		d, reason, ok := recognizeLeaf(file.Lines[leaf.Line], leaf)
		if ok {
			fresh = append(fresh, sidecarEntry{directive: d, target: leaf.Line})
			governed.Add(
				leaf.Line,
			) // a line earns one entry; a second leaf on it would double-govern at lint
		} else if reason != "" {
			skips = append(skips, skip(leaf.Line, reason))
		}
	}

	path, data, found := loadSidecar(file.Path)
	if force && found {
		sidecar, reason := forceSidecar(file, leaves, fresh, path, data)
		if reason != "" {
			skips = append(skips, AnnotateSkip{Line: -1, Reason: reason, Sidecar: path})
		}
		return sidecar, skips
	}
	if len(fresh) == 0 {
		return nil, skips // idempotent: every recognized line already has an entry
	}
	sidecar, reason := appendSidecar(fresh, path, data, found)
	if reason != "" {
		skips = append(skips, AnnotateSkip{Line: -1, Reason: reason, Sidecar: path})
	}
	return sidecar, skips
}

// annotateTextSidecar proposes the sidecar for a comment-less plain-text target
// (a pyenv .python-version file): each line auto-detection recognizes earns an
// entry carrying the inferred directive and a whole-line find locator. The line
// is the version itself, so the find is the bare version placeholder - which
// also means a second recognized line would make every locator ambiguous, and
// such a file is skipped.
func annotateTextSidecar(file scan.File) (*AnnotateSidecar, []AnnotateSkip) {
	governed := set.New[int]()
	for _, loc := range file.Found {
		if loc.Sidecar {
			governed.Add(loc.Line)
		}
	}

	var fresh []sidecarEntry
	var skips []AnnotateSkip
	for i, line := range file.Lines {
		if governed.Contains(i) {
			continue
		}
		inf, ok := match.Infer(file.Path, file.Lines, i)
		if !ok {
			continue
		}
		if reason := inf.Missing(); reason != "" {
			skips = append(skips, skip(i, reason))
			continue
		}
		d := infer.Directive(inf)
		d.Pairs = append(
			d.Pairs,
			directive.KV{Key: constant.DirectiveFind, Value: versionPlaceholder},
		)
		if reason := sidecarUnresolvedReason(inf, d, line); reason != "" {
			skips = append(skips, skip(i, reason))
			continue
		}
		fresh = append(fresh, sidecarEntry{directive: d, target: i})
	}
	if len(fresh) > 1 {
		for _, e := range fresh {
			skips = append(
				skips,
				skip(e.target, "multiple trackable lines, so a find locator would be ambiguous"),
			)
		}
		return nil, skips
	}

	path, data, found := loadSidecar(file.Path)
	generated, reason := appendSidecar(fresh, path, data, found)
	if reason != "" {
		skips = append(skips, AnnotateSkip{Line: -1, Reason: reason, Sidecar: path})
	}
	return generated, skips
}

// recognizeLeaf builds the explicit directive a JSON leaf earns, or reports ok
// false when the leaf names no trackable reference. It infers the source from the
// leaf (see [inferLeaf]), pairs the jq locator with a repository-anchored find,
// then validates exactly what run will do - the provider's resource builds and the
// find locates a version on the line - so a generated entry is one lint accepts.
func recognizeLeaf(line string, leaf sidecar.Leaf) (directive.Directive, string, bool) {
	inf, reason, ok := inferLeaf(leaf)
	if !ok {
		return directive.Directive{}, reason, false
	}
	d := explicitDirective(inf, leaf.JQ)
	if reason := sidecarUnresolvedReason(inf, d, line); reason != "" {
		return directive.Directive{}, reason, false
	}
	return d, "", true
}

// recognizedLeaf reports whether a JSON leaf looks like an annotate candidate
// before checking whether its line is opted out.
func recognizedLeaf(leaf sidecar.Leaf) bool {
	_, reason, ok := inferLeaf(leaf)
	return ok || reason != ""
}

// inferLeaf resolves the provider and parameters a JSON leaf names by feeding
// [match.Infer] a synthesized YAML line (the form the image:/uses: auto-routes
// read). Object leaves use their real key; array leaves have no key, so only a
// tag- or digest-shaped value is tried as an image reference.
func inferLeaf(leaf sidecar.Leaf) (match.Inference, string, bool) {
	syntheticLine, ok := inferenceLine(leaf)
	if !ok {
		return match.Inference{}, "", false
	}
	inf, ok := match.Infer(syntheticInferencePath, []string{syntheticLine}, 0)
	if !ok {
		return match.Inference{}, "", false
	}
	if reason := inf.Missing(); reason != "" {
		return match.Inference{}, reason, false
	}
	return inf, "", true
}

func inferenceLine(leaf sidecar.Leaf) (string, bool) {
	if leaf.Key != "" {
		return " " + leaf.Key + ": " + leaf.Value, true
	}
	if !strings.Contains(leaf.Value, ":") &&
		!strings.Contains(leaf.Value, constant.DockerDigestMarker) {
		return "", false
	}
	return " image: " + leaf.Value, true
}

// sourceKeyPairs builds the source-identifying key prefix every annotation
// carries: the provider, the registry and host when inferred, and the
// repository.
func sourceKeyPairs(inf match.Inference) []directive.KV {
	pairs := []directive.KV{{Key: constant.DirectiveProvider, Value: inf.Provider}}
	if inf.Registry != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveRegistry, Value: inf.Registry})
	}
	if inf.Host != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveHost, Value: inf.Host})
	}
	pairs = append(pairs, directive.KV{Key: constant.DirectiveRepository, Value: inf.Repository})
	if inf.TagPrefix != "" {
		pairs = append(pairs, directive.KV{Key: constant.RuleTagPrefix, Value: inf.TagPrefix})
	}
	return pairs
}

// explicitDirective builds the sidecar entry for an inferred reference: the
// resolved provider and its parameters written explicitly (a sidecar entry has no
// line adjacency to re-infer from at bind time), the jq locator that pins it to
// its line, and - for a docker image - a repository-anchored find. The find both
// disambiguates the version where a registry port adds a second number-shaped
// token and makes drift loud: if the image is later changed, the find no longer
// matches and run errors instead of tracking the wrong repository. RenderYAML
// imposes canonical key order, so the field order here is immaterial.
func explicitDirective(inf match.Inference, jqExpr string) directive.Directive {
	pairs := sourceKeyPairs(inf)
	pairs = append(pairs, directive.KV{Key: constant.DirectiveJQ, Value: jqExpr})
	if inf.Provider == constant.ProviderDocker {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveFind, Value: imageFind(inf)})
	}
	return directive.Directive{Pairs: pairs}
}

// imageFind builds the repository-anchored find for a docker image leaf:
// `<registry>/<repository>:<version>`, the form the image reference takes on the
// line, with the version as the placeholder the rewriter rewrites.
func imageFind(inf match.Inference) string {
	prefix := inf.Repository
	if inf.Registry != "" {
		prefix = inf.Registry + "/" + inf.Repository
	}
	return prefix + ":" + versionPlaceholder
}

// sidecarUnresolvedReason reports why a generated entry would not actually
// resolve, running the same offline checks lint and run perform. An empty reason
// means the entry is safe to emit.
func sidecarUnresolvedReason(inf match.Inference, d directive.Directive, line string) string {
	return infer.Unresolved(inf.Provider, d, line, func() (match.Rewriter, error) {
		return sidecarRewriter(inf, d, line)
	})
}

// sidecarRewriter mirrors the run pipeline's generated-sidecar rewriter choice.
func sidecarRewriter(
	inf match.Inference,
	d directive.Directive,
	line string,
) (match.Rewriter, error) {
	if find, has := d.Get(constant.DirectiveFind); has {
		if inf.Provider == constant.ProviderDocker &&
			strings.Contains(line, constant.DockerDigestMarker) {
			return match.NewGuarded(find, match.NewDockerPin())
		}
		return match.NewFindReplace(find, "")
	}
	return match.For(match.Context{Line: line, Provider: inf.Provider}), nil
}

// appendSidecar lays the fresh entries after an existing sidecar's bytes (or
// writes a new document when none exists), preserving every existing entry and
// any comments verbatim - the default annotate contract never rewrites what is
// already there. A structurally broken existing sidecar (one that is not a YAML
// list) is left untouched, since appending to it would compound the corruption;
// lint owns that diagnostic.
func appendSidecar(
	fresh []sidecarEntry,
	path string,
	data []byte,
	found bool,
) (*AnnotateSidecar, string) {
	if len(fresh) == 0 {
		return nil, ""
	}
	if found {
		if _, err := sidecar.Entries(data); err != nil {
			return nil, err.Error() // not a valid list: leave the broken sidecar for lint to surface
		}
	}
	chunk, err := renderEntries(fresh)
	if err != nil {
		return nil, err.Error()
	}
	content := string(chunk)
	if found {
		prefix := string(data)
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
		content = prefix + content
	} else {
		content = sidecarModeline() + content
	}
	return &AnnotateSidecar{Path: path, Content: content, Entries: entryChanges(fresh)}, ""
}

// forceSidecar repairs drift by round-tripping the existing sidecar's parsed tree:
// an entry whose line still names a recognized reference has its source keys
// re-derived (its locator and selection rules preserved), every other entry - and
// every comment - is kept verbatim, and the fresh entries are appended. A
// structurally broken existing sidecar (not a YAML list) is left untouched for
// lint, exactly as the non-force path - force repairs drift, it never clobbers. It
// returns nil when nothing drifted and nothing is new, so force stays a no-op on an
// already-correct sidecar.
func forceSidecar(
	file scan.File,
	leaves []sidecar.Leaf,
	fresh []sidecarEntry,
	path string,
	data []byte,
) (*AnnotateSidecar, string) {
	if _, err := sidecar.Entries(data); err != nil {
		return nil, err.Error() // not a valid list: leave the broken sidecar for lint, never clobber it under force
	}
	byLine := make(map[int]sidecar.Leaf, len(leaves))
	for _, leaf := range leaves {
		byLine[leaf.Line] = leaf
	}

	var updated []SidecarEntryChange
	refresh := func(line int, existing directive.Directive) (directive.Directive, bool) {
		leaf, ok := byLine[line]
		if !ok {
			return directive.Directive{}, false
		}
		inf, _, ok := inferLeaf(leaf)
		if !ok || !sourceDrifted(existing, inf) {
			return directive.Directive{}, false
		}
		updated = append(
			updated,
			SidecarEntryChange{Target: line, Provider: inf.Provider, Existing: true},
		)
		return refreshSource(existing, inf), true
	}

	directives := xslices.Map(fresh, func(e sidecarEntry) directive.Directive {
		return e.directive
	})
	content, err := sidecar.Refresh(data, file.Lines, providerKeys, refresh, directives)
	if err != nil || (len(updated) == 0 && len(fresh) == 0) {
		if err != nil {
			return nil, err.Error()
		}
		return nil, ""
	}

	changes := slices.Concat(updated, entryChanges(fresh))
	return &AnnotateSidecar{Path: path, Content: string(content), Entries: changes}, ""
}

// sourceDrifted reports whether an existing entry's source keys disagree with what
// the line now infers - the signal force should re-derive them. The host and
// product are compared only when the line infers one, mirroring [inferenceOwns]:
// an explicit value inference does not supply is deliberate, not drift.
func sourceDrifted(existing directive.Directive, inf match.Inference) bool {
	provider, _ := existing.Get(constant.DirectiveProvider)
	repository, _ := existing.Get(constant.DirectiveRepository)
	registry, _ := existing.Get(constant.DirectiveRegistry)
	host, _ := existing.Get(constant.DirectiveHost)
	product, _ := existing.Get(constant.DirectiveProduct)
	source, _ := existing.Get(constant.DirectiveSource)
	tagPrefix, _ := existing.Get(constant.RuleTagPrefix)
	return provider != inf.Provider || repository != inf.Repository ||
		registry != inf.Registry ||
		(inf.Host != "" && host != inf.Host) ||
		(inf.Product != "" && product != inf.Product) ||
		(inf.Source != "" && source != inf.Source) ||
		(inf.TagPrefix != "" && tagPrefix != inf.TagPrefix)
}

// refreshSource re-derives an entry's source keys from the line while preserving
// its locator and selection rules: the provider, repository, and registry are
// replaced, every other key (the jq/find locator, constraint, include, ...) is
// kept in its written order. The locator is preserved deliberately - it still
// resolves to the line, so only the drifted source needs repair.
func refreshSource(existing directive.Directive, inf match.Inference) directive.Directive {
	pairs := sourceKeyPairs(inf)
	for _, kv := range existing.Pairs {
		if inferenceOwns(kv.Key, inf) {
			continue // re-derived above
		}
		pairs = append(pairs, kv) // keep the locator and every selection rule
	}
	return directive.Directive{Pairs: pairs}
}

// renderEntries serializes sidecar entries to canonical YAML list bytes.
func renderEntries(entries []sidecarEntry) ([]byte, error) {
	directives := xslices.Map(entries, func(e sidecarEntry) directive.Directive {
		return e.directive
	})
	return sidecar.Render(directives, providerKeys)
}

// entryChanges builds a fresh-entry change record per entry.
func entryChanges(entries []sidecarEntry) []SidecarEntryChange {
	changes := xslices.Map(entries, func(e sidecarEntry) SidecarEntryChange {
		provider, _ := e.directive.Get(constant.DirectiveProvider)
		return SidecarEntryChange{Target: e.target, Provider: provider, Existing: false}
	})
	return changes
}

// loadSidecar finds the sidecar governing target: the first existing candidate
// name (.yaml before .yml) with its bytes, or - when none exists - the preferred
// .yaml name with found false, the path a fresh sidecar is created at.
func loadSidecar(target string) (string, []byte, bool) {
	names := sidecar.Names(target)
	for _, name := range names {
		if data, err := os.ReadFile(name); err == nil {
			return name, data, true
		}
	}
	return names[0], nil, false
}
