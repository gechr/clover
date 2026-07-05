package mode

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/gechr/clover/internal/comment"
	"github.com/gechr/clover/internal/config"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/gechr/x/set"
	xsync "github.com/gechr/x/sync"
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
// canonicalize, any rejected for an unknown key, and whether they were written.
// For a sidecar, Sidecar is set and Content carries the whole re-emitted
// document (the file is rewritten as a unit, not line by line).
type FormatFile struct {
	Path     string
	Changes  []FormatChange
	Errors   []FormatError
	Sidecar  bool
	Content  string
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

// Format canonicalizes every directive comment under roots - reordering keys
// into their canonical sequence and normalizing quoting - without resolving or
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
	parallelism int,
	opts ...pipeline.Option,
) (FormatSummary, error) {
	// The resolver is the single source of per-root config: wire it into the scan
	// itself so format applies each root's paths.exclude and required-version gate,
	// not just the fmt.prune Primary reads below.
	opts = append(opts, pipeline.WithConfig(configs))
	files, _, err := pipeline.Scan(ctx, roots, opts...)
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

	// Format each file's inline directives concurrently - the per-file work only
	// reads immutable shared state and writes its own file - keeping each result at
	// its own index. A skipped sidecar file leaves a nil slot. The cross-file
	// sidecar dedup and the line-order assembly stay sequential below, so only the
	// heavy per-file formatting runs in parallel.
	formatted := make([]*formatWork, len(files))
	xsync.Parallel(parallelism, len(files), func(i int) {
		file := files[i]
		if scan.IsSidecar(file.Path) {
			return // a sidecar's diagnostics File has no inline directives to format
		}
		w := &formatWork{file: formatFile(file, prune)}
		w.sidecar, w.hasSidecar = sidecarFor(file)
		if len(w.file.Changes) > 0 && !dry {
			lines := applyChanges(file.Lines, w.file.Changes)
			if err := writeFile(file.Path, lines); err != nil {
				w.file.WriteErr = err
			} else {
				w.file.Written = true
			}
		}
		formatted[i] = w
	})

	// A healthy sidecar is folded into its target's File, so it never appears as a
	// File of its own; format each distinct sidecar once, keyed off the targets
	// that carry its entries.
	sidecarResults := formatSidecars(formatted, prune, dry, parallelism)

	out := make([]FormatFile, 0, len(files))
	seenSidecar := set.New[string]()
	for _, w := range formatted {
		if w == nil {
			continue
		}
		// Re-emit the sidecar before the target's (absent) inline directives, on the
		// first target that carries it - mirroring the sequential emit order.
		if w.hasSidecar && !seenSidecar.Contains(w.sidecar) {
			seenSidecar.Add(w.sidecar)
			out = append(out, sidecarResults[w.sidecar])
		}
		out = append(out, w.file)
	}
	return FormatSummary{Files: out}, nil
}

// formatWork is one target file's formatted inline result plus the sidecar path
// (if any) that governs it, gathered in the parallel pass for sequential
// assembly.
type formatWork struct {
	file       FormatFile
	sidecar    string
	hasSidecar bool
}

// formatSidecars formats each distinct governing sidecar once, in parallel, and
// returns the results keyed by path. Sidecars are deduped in first-seen order so
// each is formatted (and written) exactly once.
func formatSidecars(work []*formatWork, prune, dry bool, parallelism int) map[string]FormatFile {
	var paths []string
	seen := set.New[string]()
	for _, w := range work {
		if w != nil && w.hasSidecar && !seen.Contains(w.sidecar) {
			seen.Add(w.sidecar)
			paths = append(paths, w.sidecar)
		}
	}

	results := make([]FormatFile, len(paths))
	xsync.Parallel(parallelism, len(paths), func(j int) {
		results[j] = formatSidecar(paths[j], prune, dry)
	})

	byPath := make(map[string]FormatFile, len(paths))
	for j, path := range paths {
		byPath[path] = results[j]
	}
	return byPath
}

// sidecarFor returns the sidecar path governing file's target, when file carries
// sidecar-sourced entries. The healthy sidecar's bytes are not in the scan (its
// entries were folded into this target File), so the path is recovered by probing
// the target's candidate sidecar names.
func sidecarFor(file scan.File) (string, bool) {
	hasEntry := false
	for _, loc := range file.Found {
		if loc.Sidecar {
			hasEntry = true
			break
		}
	}
	if !hasEntry {
		return "", false
	}
	for _, name := range sidecar.Names(file.Path) {
		if info, err := os.Stat(name); err == nil && info.Mode().IsRegular() {
			return name, true
		}
	}
	return "", false
}

// formatSidecar re-emits one sidecar in canonical form. A read failure or an
// unknown-key rejection is recorded as an error (format exits non-zero on it,
// exactly as for an inline directive); a structurally broken sidecar yields no
// change, since lint owns those diagnostics. The whole document is rewritten as a
// unit when it changes and dry is off.
func formatSidecar(path string, prune, dry bool) FormatFile {
	out := FormatFile{Path: path, Sidecar: true}
	data, err := os.ReadFile(path)
	if err != nil {
		out.Errors = append(
			out.Errors,
			FormatError{Err: fmt.Errorf("read sidecar %q: %w", path, err)},
		)
		return out
	}
	res, err := sidecar.Canonicalize(data, providerKeys, prune)
	if err != nil {
		out.Errors = append(out.Errors, FormatError{Err: err})
		return out
	}
	if !res.Changed {
		return out
	}
	out.Content = string(res.Content)
	out.Changes = append(out.Changes, FormatChange{New: out.Content, Pruned: res.Pruned})
	if !dry {
		if err := writeNew(path, res.Content); err != nil {
			out.WriteErr = err
		} else {
			out.Written = true
		}
	}
	return out
}

// formatFile canonicalizes each directive in file and collects the lines that
// would change. A directive carrying an unknown key is rejected (recorded as an
// error and left untouched) so a stale or mistyped key cannot ride along as
// inert configuration; with prune set the unknown keys are stripped instead and
// the strip recorded as a change.
func formatFile(file scan.File, prune bool) FormatFile {
	syntax := comment.For(file.Path)
	formatted := FormatFile{Path: file.Path}
	for _, located := range file.Found {
		if located.Sidecar {
			continue // a sidecar directive has no inline text in this file to canonicalize
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
	reordered = directive.CanonicalizeTags(reordered)
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
	return provider.KeyNames(prov)
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
