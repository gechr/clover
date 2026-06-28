package mode

import (
	"context"
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
)

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
// add or rewrite, and whether they were written.
type AnnotateFile struct {
	Path     string
	Changes  []AnnotateChange
	Written  bool
	WriteErr error
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
// under force, the existing ones to canonicalise). A line bound to an existing
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
			// An existing annotation is only canonicalised under force, and only when
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

// forceEligible reports whether an existing directive may be canonicalised under
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

// rewrite canonicalises an existing directive comment into its minimal form,
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
