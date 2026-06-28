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
)

// scanPath turns one walked file into a File. A sidecar name resolves its target
// instead, building the target's File from the sidecar entries (and any inline
// directives the target carries). A file that itself has a sidecar is suppressed
// once scanned, because its sidecar's job already produces a merged File for it.
func scanPath(job scanJob, cfg config) (File, bool) {
	if target, ok := sidecar.Target(filepath.Base(job.path)); ok {
		return scanSidecar(job.path, target, cfg)
	}

	file, ok := scanFile(job.path, job.size, cfg.maxSize, cfg.requireDirective)
	if !ok {
		return File{}, false
	}
	if hasSidecar(job.path) {
		skipFile(job.path, "scanned via sidecar").Msg("Skipping file")
		return File{}, false
	}
	return file, true
}

// scanSidecar builds the target's File from a sidecar: it reads the target with
// all the binary/size/symlink safety gates (so a write later is safe) but no
// keyword requirement, then appends the sidecar's resolved entries and errors.
// A sidecar with no target sibling, or a .yml superseded by a .yaml, is skipped.
func scanSidecar(sidecarPath, targetName string, cfg config) (File, bool) {
	dir := filepath.Dir(sidecarPath)
	targetPath := filepath.Join(dir, targetName)

	info, err := os.Stat(targetPath)
	if err != nil || !info.Mode().IsRegular() {
		skipFile(sidecarPath, "no target sibling").Msg("Skipping file")
		return File{}, false
	}
	// The walk only filtered the sidecar path; the target is reached by name, so
	// it must clear the same ignore/exclude predicate before any read or write.
	if cfg.ignore(targetPath, false) {
		skipFile(targetPath, reasonIgnored).Msg("Skipping file")
		return File{}, false
	}
	if supersededByYAML(sidecarPath, dir, targetName) {
		skipFile(sidecarPath, "superseded by .yaml sidecar").Msg("Skipping file")
		return File{}, false
	}

	file, ok := scanFile(targetPath, info.Size(), cfg.maxSize, false)
	if !ok {
		return File{}, false // the target failed a safety gate; nothing to write
	}

	located, errs := resolveSidecar(sidecarPath, file)
	file.Found = append(file.Found, located...)
	file.Errors = append(file.Errors, errs...)
	slices.SortFunc(file.Found, func(a, b Located) int { return cmp.Compare(a.Line, b.Line) })
	return file, true
}

// resolveSidecar parses the sidecar and resolves each entry to a target line,
// applying the conflict rules: an entry resolving to a clover:ignore-suppressed
// line is dropped with a warning (the local opt-out wins), and an entry resolving
// to a line another directive already governs is double-governance - an errored
// marker, never a silent second write.
func resolveSidecar(sidecarPath string, file File) ([]Located, []LineError) {
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return nil, []LineError{{Line: 0, Err: fmt.Errorf("read sidecar %q: %w", sidecarPath, err)}}
	}
	entries, err := sidecar.Entries(data)
	if err != nil {
		return nil, []LineError{{Line: 0, Err: err}}
	}

	governed := governedLines(file.Found)
	var (
		located []Located
		errs    []LineError
	)
	for _, entry := range entries {
		fail := func(line int, err error) {
			errs = append(
				errs,
				LineError{
					Line: line,
					Err:  fmt.Errorf("sidecar entry at line %d: %w", entry.Line, err),
				},
			)
		}
		if entry.Err != nil {
			fail(0, entry.Err)
			continue
		}
		line, err := sidecar.Locate(file.Lines, entry.Directive)
		if err != nil {
			fail(0, err)
			continue
		}
		if file.Ignored[line] {
			clog.Warn().
				Path(field.Path, file.Path).
				Int(field.Line, line+1).
				Msg("Sidecar entry targets a clover:ignore line; skipping")
			continue
		}
		if governed[line] {
			fail(line, fmt.Errorf("targets line %d, already governed by another directive", line+1))
			continue
		}
		governed[line] = true
		located = append(located, Located{Line: line, Directive: entry.Directive, Sidecar: true})
	}
	return located, errs
}

// governedLines marks the lines an inline directive owns - its comment line and
// the target below it - so a sidecar entry resolving onto either is caught as
// double-governance rather than silently applied alongside.
func governedLines(found []Located) map[int]bool {
	governed := make(map[int]bool, len(found))
	for _, loc := range found {
		governed[loc.Line] = true
		governed[loc.Line+1] = true
	}
	return governed
}

// hasSidecar reports whether path is the target of an existing sidecar, so the
// walk can leave it to the sidecar's job rather than emit a duplicate File.
func hasSidecar(path string) bool {
	for _, name := range sidecar.Names(path) {
		if info, err := os.Stat(name); err == nil && info.Mode().IsRegular() {
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
	info, err := os.Stat(filepath.Join(dir, preferred))
	return err == nil && info.Mode().IsRegular()
}
