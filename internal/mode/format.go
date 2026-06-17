package mode

import (
	"context"

	"github.com/gechr/cusp/internal/comment"
	"github.com/gechr/cusp/internal/constant"
	"github.com/gechr/cusp/internal/directive"
	"github.com/gechr/cusp/internal/pipeline"
	"github.com/gechr/cusp/internal/provider"
	"github.com/gechr/cusp/internal/scan"
)

// FormatChange is a directive comment line format would rewrite: its 0-based
// line index and the before and after text.
type FormatChange struct {
	Line int
	Old  string
	New  string
}

// FormatFile is the format outcome for one file: the comment lines it would
// canonicalise and, unless checking, whether they were written.
type FormatFile struct {
	Path     string
	Changes  []FormatChange
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

// OK reports whether every directive is already canonical - the signal a
// format --check gate exits on.
func (s FormatSummary) OK() bool { return s.Changed() == 0 }

// Format canonicalises every directive comment under roots - reordering keys
// into their canonical sequence and normalising quoting - without resolving or
// touching the version. It only ever rewrites the comment, never the target
// line, and is idempotent. With check set it reports what would change and
// writes nothing, the read-only CI gate; otherwise it rewrites each changed file
// atomically.
func Format(
	ctx context.Context,
	roots []string,
	check bool,
	opts ...pipeline.Option,
) (FormatSummary, error) {
	files, err := pipeline.Scan(ctx, roots, opts...)
	if err != nil {
		return FormatSummary{}, err
	}

	out := make([]FormatFile, 0, len(files))
	for _, file := range files {
		formatted := formatFile(file)
		if len(formatted.Changes) > 0 && !check {
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
// would change.
func formatFile(file scan.File) FormatFile {
	syntax := comment.For(file.Path)
	formatted := FormatFile{Path: file.Path}
	for _, located := range file.Found {
		old := file.Lines[located.Line]
		canonical, changed := canonicalLine(old, syntax, located.Directive)
		if changed {
			formatted.Changes = append(formatted.Changes, FormatChange{
				Line: located.Line,
				Old:  old,
				New:  canonical,
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
