package mode

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gechr/clover/internal/comment"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/sidecar"
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

// strictJSON reports whether path is a strict-JSON file - one that cannot host an
// inline comment, so a directive for it must live in a sidecar. The comment-
// hosting JSON dialects (.jsonc, .json5) are excluded: they take inline comments
// like any other commentable format.
func strictJSON(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

// AnnotateChange is one comment line annotate would write: At is the 0-based
// line index it acts on, Line the comment text. Existing distinguishes the two
// shapes - a fresh insertion above a recognized line (Existing false), or a
// rewrite of an existing directive comment at At into its canonical minimal form
// (Existing true, only under --force).
type AnnotateChange struct {
	At       int
	Line     string
	Existing bool
}

// AnnotateFile is the annotate outcome for one file: the comment lines it would
// add or rewrite (for a commentable file), or the sidecar it would generate (for
// a comment-less strict-JSON target), and whether they were written. The two are
// mutually exclusive - a file either hosts inline comments or it does not.
type AnnotateFile struct {
	Path     string
	Changes  []AnnotateChange
	Sidecar  *AnnotateSidecar
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
// rewrite: the 0-based target line in the target file it governs, and whether it
// replaces an existing entry rather than introducing a new one.
type SidecarEntryChange struct {
	Target   int
	Existing bool
}

// AnnotateSummary is the annotate outcome over all roots, in file order.
type AnnotateSummary struct {
	Files []AnnotateFile
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
// already track but is not yet annotated, proposes a `clover: provider=auto`
// comment above it - the inverse of the auto-detection that resolves such a
// marker. It is the onboarding command: it leans on the same dispatch routes
// [match.Infer] uses, so a line only earns an annotation that is guaranteed to
// re-resolve. By default it never touches an existing annotation; with force it
// also rewrites a recognized line's directive into its canonical minimal form,
// collapsing keys inference supplies while preserving every rule key. With write
// off it reports what it would do and writes nothing; otherwise it rewrites each
// changed file atomically.
func Annotate(
	ctx context.Context,
	roots []string,
	write bool,
	force bool,
	configs *config.Resolver,
	opts ...pipeline.Option,
) (AnnotateSummary, error) {
	opts = append(opts,
		pipeline.WithConfig(configs),
		pipeline.WithRequireDirective(false),
	)
	files, err := pipeline.Scan(ctx, roots, opts...)
	if err != nil {
		return AnnotateSummary{}, err
	}

	out := make([]AnnotateFile, 0, len(files))
	for _, file := range files {
		if scan.IsSidecar(file.Path) {
			continue // never propose inline directives inside a sidecar file
		}
		// A strict-JSON target cannot host an inline comment, so a recognized line
		// earns a sidecar entry instead of a comment that would corrupt the JSON.
		if strictJSON(file.Path) {
			annotated := AnnotateFile{Path: file.Path, Sidecar: annotateSidecar(file, force)}
			if annotated.Sidecar != nil && write {
				if err := writeSidecar(annotated.Sidecar); err != nil {
					annotated.WriteErr = err
				} else {
					annotated.Written = true
				}
			}
			out = append(out, annotated)
			continue
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
		out = append(out, annotated)
	}
	return AnnotateSummary{Files: out}, nil
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
	governed := map[int]bool{}
	existing := map[int]scan.Located{}
	for _, loc := range file.Found {
		if loc.Sidecar {
			governed[loc.Line] = true // the sidecar already rewrites this line; never re-annotate it
			continue
		}
		governed[loc.Line] = true
		governed[loc.Line+1] = true
		existing[loc.Line+1] = loc
	}

	for i, line := range file.Lines {
		if file.Ignored[i] {
			continue // a clover:ignore control opts this line out
		}
		// A commented-out example (# - uses: …, # image: …) is documentation, not a
		// live field, so it is never a target - inference reads raw line text and
		// would otherwise match inside the comment.
		if isComment(syntax, line) {
			continue
		}
		inf, ok := match.Infer(file.Path, line)
		// Every auto route identifies its source by repository (github owner/name,
		// docker image path); an empty one means the line matched a route shape but
		// carries no usable reference, so a provider=auto there could not resolve.
		if !ok || inf.Repository == "" {
			continue
		}
		// Verify before annotating: a recognized shape may still not actually resolve
		// (a malformed image ref, FROM with no tag, a uses: pin with no version
		// comment). Gate on the same offline checks lint runs so annotate never emits
		// a directive lint would reject.
		if !resolvable(file.Path, inf, line) {
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
			if change, ok := rewrite(syntax, file.Lines[loc.Line], loc); ok {
				annotated.Changes = append(annotated.Changes, change)
			}
			continue
		}
		if governed[i] {
			continue
		}
		if change, ok := insert(syntax, i, line); ok {
			annotated.Changes = append(annotated.Changes, change)
		}
	}
	return annotated
}

// resolvable reports whether the directive annotate would write for this line
// passes the offline checks lint runs: the inferred provider exists, builds a
// valid resource, and its rewriter locates a trackable version. It is the
// verify-before-write gate - a line clover recognizes by shape but cannot
// actually resolve is never annotated, so annotate never emits a directive lint
// would reject.
func resolvable(path string, inf match.Inference, line string) bool {
	prov, ok := provider.Get(inf.Provider)
	if !ok {
		return false
	}
	if _, err := prov.Resource(inferredDirective(inf)); err != nil {
		return false
	}
	_, err := match.For(match.Context{Path: path, Line: line, Provider: inf.Provider}).Locate(line)
	return err == nil
}

// inferredDirective builds the directive the pipeline binds for a provider=auto
// marker: the inferred provider plus the parameters read from the line. It is
// what resolvable validates the provider resource against.
func inferredDirective(inf match.Inference) directive.Directive {
	pairs := []directive.KV{{Key: constant.DirectiveProvider, Value: inf.Provider}}
	if inf.Repository != "" {
		pairs = append(
			pairs,
			directive.KV{Key: constant.DirectiveRepository, Value: inf.Repository},
		)
	}
	if inf.Registry != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveRegistry, Value: inf.Registry})
	}
	return directive.Directive{Pairs: pairs}
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

// isComment reports whether line is wholly a comment - its first non-blank token
// is a comment marker - so a commented-out example is never treated as a target.
func isComment(syntax comment.Syntax, line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	for _, marker := range syntax.Line {
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	for _, block := range syntax.Blocks {
		if strings.HasPrefix(trimmed, block.Open) {
			return true
		}
	}
	return false
}

// insert builds the fresh annotation for a recognized, unannotated line: a
// `clover: provider=auto` comment indented to match the line. ok is false when
// the file's syntax exposes no comment delimiter.
func insert(syntax comment.Syntax, i int, line string) (AnnotateChange, bool) {
	body := directive.Render(canonicalDirective(directive.Directive{}))
	comment, ok := syntax.Comment(leadingWhitespace(line), body)
	if !ok {
		return AnnotateChange{}, false
	}
	return AnnotateChange{At: i, Line: comment}, true
}

// rewrite canonicalizes an existing directive comment into its minimal form,
// returning the rewritten line. ok is false when the comment cannot be
// re-rendered or the canonical form is identical to what is already there.
func rewrite(syntax comment.Syntax, line string, loc scan.Located) (AnnotateChange, bool) {
	body := directive.Render(canonicalDirective(loc.Directive))
	rendered, ok := syntax.Render(line, body)
	if !ok || rendered == line {
		return AnnotateChange{}, false
	}
	return AnnotateChange{At: loc.Line, Line: rendered, Existing: true}, true
}

// canonicalDirective returns the minimal directive annotate writes for a
// recognized line: provider=auto first, then every key from the existing
// directive except the three auto-detection supplies from the line itself
// (provider, repository, registry), kept in their original order. For a fresh
// insertion the input is empty, so the result is just provider=auto. Dropping
// repository/registry is what lets force both shed a redundant explicit value
// and repair one that has drifted from its line, while every rule key survives.
func canonicalDirective(d directive.Directive) directive.Directive {
	pairs := []directive.KV{{Key: constant.DirectiveProvider, Value: constant.ProviderAuto}}
	for _, kv := range d.Pairs {
		switch kv.Key {
		case constant.DirectiveProvider, constant.DirectiveRepository, constant.DirectiveRegistry:
			continue
		default:
			pairs = append(pairs, kv)
		}
	}
	return directive.Directive{Pairs: pairs}
}

// applyAnnotations returns a copy of lines with each change applied. Insertions
// shift every line below them, so changes are applied highest index first: each
// splice then leaves the lower indices it has not reached still valid.
func applyAnnotations(lines []string, changes []AnnotateChange) []string {
	out := make([]string, len(lines))
	copy(out, lines)

	ordered := slices.Clone(changes)
	slices.SortFunc(ordered, func(a, b AnnotateChange) int { return b.At - a.At })
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
// synthesised comment aligns with the line it annotates.
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
func annotateSidecar(file scan.File, force bool) *AnnotateSidecar {
	source := []byte(strings.Join(file.Lines, "\n"))
	leaves, err := sidecar.Leaves(source)
	if err != nil {
		return nil // not valid JSON: there is no locatable structure to track
	}

	governed := map[int]bool{}
	for _, loc := range file.Found {
		if loc.Sidecar {
			governed[loc.Line] = true
		}
	}

	var fresh []sidecarEntry
	for _, leaf := range leaves {
		if leaf.Line >= len(file.Lines) || file.Ignored[leaf.Line] || governed[leaf.Line] {
			continue
		}
		if d, ok := recognizeLeaf(file.Lines[leaf.Line], leaf); ok {
			fresh = append(fresh, sidecarEntry{directive: d, target: leaf.Line})
			governed[leaf.Line] = true // a line earns one entry; a second leaf on it would double-govern at lint
		}
	}

	path, data, found := loadSidecar(file.Path)
	if force && found {
		return forceSidecar(file, leaves, fresh, path, data)
	}
	if len(fresh) == 0 {
		return nil // idempotent: every recognized line already has an entry
	}
	return appendSidecar(fresh, path, data, found)
}

// recognizeLeaf builds the explicit directive a JSON leaf earns, or reports ok
// false when the leaf names no trackable reference. It infers the source from the
// leaf (see [inferLeaf]), pairs the jq locator with a repository-anchored find,
// then validates exactly what run will do - the provider's resource builds and the
// find locates a version on the line - so a generated entry is one lint accepts.
func recognizeLeaf(line string, leaf sidecar.Leaf) (directive.Directive, bool) {
	inf, ok := inferLeaf(leaf)
	if !ok {
		return directive.Directive{}, false
	}
	d := explicitDirective(inf, leaf.JQ)
	if !sidecarResolvable(inf, d, line) {
		return directive.Directive{}, false
	}
	return d, true
}

// inferLeaf resolves the provider and parameters a JSON leaf names by feeding
// [match.Infer] a synthesized `<key>: <value>` YAML line (the form the
// image:/uses: auto-routes read). A pinned reference (one carrying an @digest or
// @sha) is rejected: its secure pin needs a pin-aware rewriter, which is not yet
// available on a JSON string value, so annotating it would leave the pin stale.
func inferLeaf(leaf sidecar.Leaf) (match.Inference, bool) {
	if strings.Contains(leaf.Value, "@") {
		return match.Inference{}, false
	}
	inf, ok := match.Infer(syntheticInferencePath, " "+leaf.Key+": "+leaf.Value)
	if !ok || inf.Repository == "" {
		return match.Inference{}, false
	}
	return inf, true
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
	pairs := []directive.KV{{Key: constant.DirectiveProvider, Value: inf.Provider}}
	if inf.Registry != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveRegistry, Value: inf.Registry})
	}
	pairs = append(pairs, directive.KV{Key: constant.DirectiveRepository, Value: inf.Repository})
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

// sidecarResolvable reports whether a generated entry will actually resolve,
// running the same offline checks lint and run perform: the provider builds its
// resource, and the entry's rewriter locates a version on the line. It mirrors
// run's rewriter choice - a find drives the find/replace rewriter (the path a
// generated docker entry takes), otherwise the route-based dispatch - so a line a
// registry port would make ambiguous for the smart locator still validates via
// its find anchor.
func sidecarResolvable(inf match.Inference, d directive.Directive, line string) bool {
	prov, ok := provider.Get(inf.Provider)
	if !ok {
		return false
	}
	if _, err := prov.Resource(d); err != nil {
		return false
	}
	if find, has := d.Get(constant.DirectiveFind); has {
		rewriter, err := match.NewFindReplace(find, "")
		if err != nil {
			return false
		}
		_, err = rewriter.Locate(line)
		return err == nil
	}
	_, err := match.For(match.Context{Line: line, Provider: inf.Provider}).Locate(line)
	return err == nil
}

// appendSidecar lays the fresh entries after an existing sidecar's bytes (or
// writes a new document when none exists), preserving every existing entry and
// any comments verbatim - the default annotate contract never rewrites what is
// already there. A structurally broken existing sidecar (one that is not a YAML
// list) is left untouched, since appending to it would compound the corruption;
// lint owns that diagnostic.
func appendSidecar(fresh []sidecarEntry, path string, data []byte, found bool) *AnnotateSidecar {
	if len(fresh) == 0 {
		return nil
	}
	if found {
		if _, err := sidecar.Entries(data); err != nil {
			return nil // not a valid list: leave the broken sidecar for lint to surface
		}
	}
	chunk, err := renderEntries(fresh)
	if err != nil {
		return nil
	}
	content := string(chunk)
	if found {
		prefix := string(data)
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
		content = prefix + content
	}
	return &AnnotateSidecar{Path: path, Content: content, Entries: entryChanges(fresh)}
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
) *AnnotateSidecar {
	if _, err := sidecar.Entries(data); err != nil {
		return nil // not a valid list: leave the broken sidecar for lint, never clobber it under force
	}
	byLine := make(map[int]sidecar.Leaf, len(leaves))
	for _, leaf := range leaves {
		byLine[leaf.Line] = leaf
	}

	var updated []int
	refresh := func(line int, existing directive.Directive) (directive.Directive, bool) {
		leaf, ok := byLine[line]
		if !ok {
			return directive.Directive{}, false
		}
		inf, ok := inferLeaf(leaf)
		if !ok || !sourceDrifted(existing, inf) {
			return directive.Directive{}, false
		}
		updated = append(updated, line)
		return refreshSource(existing, inf), true
	}

	directives := make([]directive.Directive, len(fresh))
	for i, e := range fresh {
		directives[i] = e.directive
	}
	content, err := sidecar.Refresh(data, file.Lines, providerKeys, refresh, directives)
	if err != nil || (len(updated) == 0 && len(fresh) == 0) {
		return nil
	}

	changes := make([]SidecarEntryChange, 0, len(updated)+len(fresh))
	for _, line := range updated {
		changes = append(changes, SidecarEntryChange{Target: line, Existing: true})
	}
	for _, e := range fresh {
		changes = append(changes, SidecarEntryChange{Target: e.target, Existing: false})
	}
	return &AnnotateSidecar{Path: path, Content: string(content), Entries: changes}
}

// sourceDrifted reports whether an existing entry's source keys disagree with what
// the line now infers - the signal force should re-derive them.
func sourceDrifted(existing directive.Directive, inf match.Inference) bool {
	provider, _ := existing.Get(constant.DirectiveProvider)
	repository, _ := existing.Get(constant.DirectiveRepository)
	registry, _ := existing.Get(constant.DirectiveRegistry)
	return provider != inf.Provider || repository != inf.Repository || registry != inf.Registry
}

// refreshSource re-derives an entry's source keys from the line while preserving
// its locator and selection rules: the provider, repository, and registry are
// replaced, every other key (the jq/find locator, constraint, include, ...) is
// kept in its written order. The locator is preserved deliberately - it still
// resolves to the line, so only the drifted source needs repair.
func refreshSource(existing directive.Directive, inf match.Inference) directive.Directive {
	pairs := []directive.KV{{Key: constant.DirectiveProvider, Value: inf.Provider}}
	if inf.Registry != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveRegistry, Value: inf.Registry})
	}
	pairs = append(pairs, directive.KV{Key: constant.DirectiveRepository, Value: inf.Repository})
	for _, kv := range existing.Pairs {
		switch kv.Key {
		case constant.DirectiveProvider,
			constant.DirectiveRepository,
			constant.DirectiveRegistry:
			continue // re-derived above
		default:
			pairs = append(pairs, kv) // keep the locator and every selection rule
		}
	}
	return directive.Directive{Pairs: pairs}
}

// renderEntries serializes sidecar entries to canonical YAML list bytes.
func renderEntries(entries []sidecarEntry) ([]byte, error) {
	directives := make([]directive.Directive, len(entries))
	for i, e := range entries {
		directives[i] = e.directive
	}
	return sidecar.Render(directives, providerKeys)
}

// entryChanges builds a fresh-entry change record per entry.
func entryChanges(entries []sidecarEntry) []SidecarEntryChange {
	changes := make([]SidecarEntryChange, len(entries))
	for i, e := range entries {
		changes[i] = SidecarEntryChange{Target: e.target, Existing: false}
	}
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
