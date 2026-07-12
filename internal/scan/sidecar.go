package scan

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/sidecar"
	xos "github.com/gechr/x/os"
	"github.com/gechr/x/set"
)

// scanPath turns one walked file into zero or more Files. A sidecar name resolves
// its target, yielding the target's File (with the sidecar's resolved entries)
// and, when the sidecar is broken, a separate diagnostics File keyed on the
// sidecar path so its problems report against the sidecar, not the target. A
// file that itself has a sidecar is suppressed once scanned, because its
// sidecar's job already produces a File for it.
func scanPath(job scanJob, cfg config) []File {
	if target, ok := sidecar.Target(filepath.Base(job.path)); ok {
		return scanSidecar(job.path, target, cfg)
	}

	file, ok := scanFile(job.path, job.size, cfg.maxSize, cfg.requireDirective)
	if !ok {
		return nil
	}
	if hasSidecar(job.path) {
		skipFile(job.path, "scanned via sidecar").Msg("Skipping file")
		return nil
	}
	return []File{file}
}

// scanSidecar resolves a sidecar against its target. It reads the target with all
// the binary/size/symlink safety gates (so a write later is safe) but no keyword
// requirement, attaches the sidecar's resolved entries to the target File, and
// collects any structural problems into a separate diagnostics File. A dangling
// sidecar (no target sibling) yields only the diagnostics File; a .yml superseded
// by a .yaml is warned and dropped.
func scanSidecar(sidecarPath, targetName string, cfg config) []File {
	dir := filepath.Dir(sidecarPath)
	targetPath := filepath.Join(dir, targetName)

	if supersededByYAML(sidecarPath, dir, targetName) {
		clog.Warn().
			Path(field.Path, sidecarPath).
			Msg("Sidecar ignored: a .yaml sibling takes precedence over .yml")
		return nil
	}

	data, lines, readErr := readSidecar(sidecarPath)

	info, err := os.Stat(targetPath)
	if err != nil || !info.Mode().IsRegular() {
		return []File{diagnose(sidecarPath, lines, []LineError{{
			Sidecar: true,
			Err:     fmt.Errorf("references missing target %q", targetName),
		}})}
	}
	// The walk only filtered the sidecar path; the target is reached by name, so
	// it must clear the same ignore/exclude predicate before any read or write.
	if cfg.ignore(targetPath, false) {
		skipFile(targetPath, reasonIgnored).Msg("Skipping file")
		return nil
	}

	file, ok := scanFile(targetPath, info.Size(), cfg.maxSize, false)
	if !ok {
		return nil // the target failed a safety gate; nothing to write
	}
	if readErr != nil {
		return []File{
			file,
			diagnose(sidecarPath, lines, []LineError{{Sidecar: true, Err: readErr}}),
		}
	}

	located, errs := resolveSidecar(data, file)
	file.Found = append(file.Found, located...)
	slices.SortFunc(file.Found, func(a, b Located) int { return cmp.Compare(a.Line, b.Line) })

	out := []File{file}
	if len(errs) > 0 {
		out = append(out, diagnose(sidecarPath, lines, errs))
	}
	return out
}

// diagnose builds the diagnostics File for a broken sidecar: keyed on the sidecar
// path (so findings report against it) with its own lines for context, carrying
// only the structural errors.
func diagnose(sidecarPath string, lines []string, errs []LineError) File {
	return File{Path: sidecarPath, Lines: lines, Errors: errs}
}

// readSidecar reads a sidecar's bytes and its LF-split lines. A read failure is
// returned for the caller to surface as a diagnostic rather than aborting.
func readSidecar(sidecarPath string) ([]byte, []string, error) {
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read sidecar %q: %w", sidecarPath, err)
	}
	return data, splitLines(data), nil
}

// resolveSidecar parses the sidecar bytes and resolves each entry to a target
// line, applying the conflict rules: an entry resolving to a clover:ignore-
// suppressed line is dropped with a warning (the local opt-out wins), and an
// entry resolving to a line another directive already governs is double-
// governance - an error, never a silent second write. Every problem is tagged
// Sidecar so lint fails on it while run downgrades it to a skip.
func resolveSidecar(data []byte, file File) ([]Located, []LineError) {
	entries, err := sidecar.Entries(data)
	if err != nil {
		return nil, []LineError{{Sidecar: true, Err: err}}
	}

	governed := governedLines(file.Found, file.Lines)
	locator := sidecar.NewLocator(file.Lines)
	var (
		located []Located
		errs    []LineError
	)
	for _, entry := range entries {
		fail := func(err error) {
			errs = append(errs, LineError{
				Line:    entry.Line - 1, // the entry's own line in the sidecar
				Sidecar: true,
				Err:     fmt.Errorf("entry at line %d: %w", entry.Line, err),
			})
		}
		if entry.Err != nil {
			fail(entry.Err)
			continue
		}
		line, err := locator.Locate(entry.Directive)
		if err != nil {
			fail(err)
			continue
		}
		if file.Ignored.Contains(line) {
			// The local clover:ignore wins, but surface the suppressed entry as a
			// visible skip rather than dropping it silently.
			errs = append(errs, LineError{
				Line:    entry.Line - 1,
				Sidecar: true,
				Skip:    true,
				Err: fmt.Errorf(
					"entry at line %d: target line %d is suppressed by clover:ignore",
					entry.Line, line+1,
				),
			})
			continue
		}
		if governed.Contains(line) {
			fail(fmt.Errorf("targets line %d, already governed by another directive", line+1))
			continue
		}
		governed.Add(line)
		located = append(located, Located{Line: line, Directive: entry.Directive, Sidecar: true})
	}
	return located, errs
}

// governedLines marks the lines an inline directive owns - its comment line and
// the line it targets (the next line, or its target= match) - so a sidecar
// entry resolving onto either is caught as double-governance rather than
// silently applied alongside. A directive whose target does not resolve owns
// only its comment line; its own validation reports the fault.
func governedLines(found []Located, lines []string) set.Set[int] {
	governed := make(set.Set[int], len(found))
	for _, loc := range found {
		governed.Add(loc.Line)
		if target, err := loc.Target(lines); err == nil {
			governed.Add(target)
		}
	}
	return governed
}

// IsSidecar reports whether path names a sidecar file. format and annotate use
// it to skip the diagnostics File a broken sidecar produces - a sidecar is never
// an inline-directive target to reformat or annotate.
func IsSidecar(path string) bool {
	_, ok := sidecar.Target(filepath.Base(path))
	return ok
}

// sidecarRootsFor returns the existing sidecar files for any file root, so a scan
// rooted directly at a target discovers its sidecar - a single-file root's walk
// would otherwise never visit the sibling. Directory roots are skipped (their
// walk already visits siblings).
func sidecarRootsFor(roots []string) []string {
	var extra []string
	for _, root := range roots {
		if info, err := os.Stat(root); err != nil || info.IsDir() {
			continue
		}
		for _, name := range sidecar.Names(root) {
			if ok, _ := xos.IsFile(name); ok {
				extra = append(extra, name)
			}
		}
	}
	return extra
}

// hasSidecar reports whether path is the target of an existing sidecar, so the
// walk can leave it to the sidecar's job rather than emit a duplicate File.
func hasSidecar(path string) bool {
	for _, name := range sidecar.Names(path) {
		if ok, _ := xos.IsFile(name); ok {
			return true
		}
	}
	return false
}

// supersededByYAML reports whether sidecarPath is a .yml whose .yaml sibling also
// exists; the .yaml wins, so the .yml is ignored.
func supersededByYAML(sidecarPath, dir, targetName string) bool {
	preferred := sidecar.Names(targetName)[0] // the .yaml variant
	if filepath.Base(sidecarPath) == preferred {
		return false
	}
	ok, _ := xos.IsFile(filepath.Join(dir, preferred))
	return ok
}
