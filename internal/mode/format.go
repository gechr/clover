package mode

import (
	"cmp"
	"context"

	"github.com/gechr/clover/internal/comment"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/scan"
)

// FormatChange is a directive comment line format would rewrite: its 0-based
// line index, the before and after text, and any unknown keys --prune removed.
type FormatChange struct {
	Line   int
	Old    string
	New    string
	Pruned []string
}

// FormatError is a directive format left untouched because it carries an unknown
// key, reported (and exited on) rather than silently reformatted. --prune turns
// these into changes instead.
type FormatError struct {
	Line int
	Err  error
}

// FormatFile is the format outcome for one file: the comment lines it would
// canonicalise, any rejected for an unknown key, and whether they were written.
type FormatFile struct {
	Path     string
	Changes  []FormatChange
	Errors   []FormatError
	Written  bool
	WriteErr error
}

// FormatSummary is the format outcome over all roots, in file order.
type FormatSummary struct {
	Files []FormatFile
}

// Changed reports the total number of directive comments format would rewrite.
func (s FormatSummary) Changed() int {
	var n int
	for _, f := range s.Files {
		n += len(f.Changes)
	}
	return n
}

// Errored reports the total number of directives format rejected for an unknown
// key (always zero under --prune, which removes them instead).
func (s FormatSummary) Errored() int {
	var n int
	for _, f := range s.Files {
		n += len(f.Errors)
	}
	return n
}

// OK reports whether every directive is already canonical and carries no unknown
// key - the signal a format --check gate, and an unknown-key rejection, exit on.
func (s FormatSummary) OK() bool { return s.Changed() == 0 && s.Errored() == 0 }

// Format canonicalises every directive comment under roots - reordering keys
// into their canonical sequence and normalising quoting - without resolving or
// touching the version. It only ever rewrites the comment, never the target
// line, and is idempotent. With dry set it reports what would change and writes
// nothing, the read-only path shared by --check and --dry-run; otherwise it
// rewrites each changed file atomically.
func Format(
	ctx context.Context,
	roots []string,
	dry bool,
	cliPrune *bool,
	configs *config.Resolver,
	opts ...pipeline.Option,
) (FormatSummary, error) {
	// The resolver is the single source of per-root config: wire it into the scan
	// itself so format applies each root's paths.exclude and required-version gate,
	// not just the fmt.prune Primary reads below.
	opts = append(opts, pipeline.WithConfig(configs))
	files, err := pipeline.Scan(ctx, roots, opts...)
	if err != nil {
		return FormatSummary{}, err
	}

	// Prune is per-invocation: a CLI --prune/--no-prune wins, else the primary
	// root's fmt.prune - the sole repo's config, or the user default across a
	// multi-repo scan - resolved after the scan has settled which roots it spans.
	prune := false
	if p := cmp.Or(cliPrune, configs.Primary().Prune()); p != nil {
		prune = *p
	}

	out := make([]FormatFile, 0, len(files))
	for _, file := range files {
		if scan.IsSidecar(file.Path) {
			continue // a sidecar's diagnostics File has no inline directives to format
		}
		formatted := formatFile(file, prune)
		if len(formatted.Changes) > 0 && !dry {
			lines := applyChanges(file.Lines, formatted.Changes)
			if err := writeFile(file.Path, lines); err != nil {
				formatted.WriteErr = err
			} else {
				formatted.Written = true
			}
		}
		out = append(out, formatted)
	}
	return FormatSummary{Files: out}, nil
}

// formatFile canonicalises each directive in file and collects the lines that
// would change. A directive carrying an unknown key is rejected (recorded as an
// error and left untouched) so a stale or mistyped key cannot ride along as
// inert configuration; with prune set the unknown keys are stripped instead and
// the strip recorded as a change.
func formatFile(file scan.File, prune bool) FormatFile {
	syntax := comment.For(file.Path)
	formatted := FormatFile{Path: file.Path}
	for _, located := range file.Found {
		if located.Sidecar {
			continue // a sidecar directive has no inline text in this file to canonicalise
		}
		d := located.Directive
		name, _ := d.Get(constant.DirectiveProvider)
		var pruned []string
		if prune {
			d, pruned = d.PruneUnknownKeys(providerKeys(name))
		} else if err := d.CheckKeys(providerKeys(name)); err != nil {
			formatted.Errors = append(formatted.Errors, FormatError{Line: located.Line, Err: err})
			continue
		}

		old := file.Lines[located.Line]
		canonical, changed := canonicalLine(old, syntax, d)
		if changed {
			formatted.Changes = append(formatted.Changes, FormatChange{
				Line:   located.Line,
				Old:    old,
				New:    canonical,
				Pruned: pruned,
			})
		}
	}
	return formatted
}

// canonicalLine reorders and re-renders the directive on line into its canonical
// form, splicing it back through the comment delimiters. It reports whether the
// result differs from the original.
func canonicalLine(line string, syntax comment.Syntax, d directive.Directive) (string, bool) {
	name, _ := d.Get(constant.DirectiveProvider)
	reordered := directive.Reorder(d, providerKeys(name))
	reordered = directive.CanonicaliseTags(reordered)
	body := directive.Render(reordered)

	rendered, ok := syntax.Render(line, body)
	if !ok {
		return line, false
	}
	return rendered, rendered != line
}

// providerKeys returns the directive keys a provider declares, in its canonical
// order, or nil when the provider is unknown (a follower, or a typo) - in which
// case the marker's keys all fall into the common zones.
func providerKeys(name string) []string {
	prov, ok := provider.Get(name)
	if !ok {
		return nil
	}
	keys := prov.Keys()
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = key.Name
	}
	return names
}

// applyChanges returns a copy of lines with each change spliced onto its line.
func applyChanges(lines []string, changes []FormatChange) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	for _, change := range changes {
		if change.Line >= 0 && change.Line < len(out) {
			out[change.Line] = change.New
		}
	}
	return out
}
