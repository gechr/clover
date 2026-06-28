package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/log/field"
	xfilepath "github.com/gechr/x/filepath"
)

const (
	reasonIgnored    = "ignored"
	reasonNonRegular = "non-regular"
	reasonSymlink    = "symlink"
	reasonUnreadable = "unreadable"
	reasonVCS        = "vcs"
)

// defaultMaxSize caps the file size scan will read, so a stray large file never
// stalls a run.
const defaultMaxSize = 5 << 20 // 5 MiB

// vcsDirs are always skipped during the walk.
var vcsDirs = map[string]bool{
	".git": true,
	".jj":  true,
	".hg":  true,
	".svn": true,
}

type scanJob struct {
	path string
	size int64
}

// Scan walks roots and returns the files carrying a clover: directive, sorted by
// path for deterministic output, along with the total number of files examined.
// A single walker produces paths that a pool of workers reads and scans
// concurrently.
func Scan(ctx context.Context, roots []string, opts ...Option) ([]File, int, error) {
	cfg := config{
		workers:          runtime.NumCPU(),
		maxSize:          defaultMaxSize,
		ignore:           IgnoreFunc(func(string, bool) bool { return false }),
		requireDirective: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.workers < 1 {
		cfg.workers = 1
	}

	// Coalesce overlapping roots (exact duplicates and paths nested under another)
	// so a file reachable from two roots is walked once. Symlinks are skipped in
	// the walk, so a lexical merge suffices - no two surviving roots can alias.
	roots = xfilepath.Merge(roots)
	warnMissingRoots(roots)

	jobs := make(chan scanJob)
	var walkErr error
	go func() {
		defer close(jobs)
		for _, root := range roots {
			if err := walk(ctx, root, cfg, jobs); err != nil {
				walkErr = err
				return
			}
		}
	}()

	var (
		mu      sync.Mutex
		files   []File
		scanned atomic.Int64
		wg      sync.WaitGroup
	)
	for range cfg.workers {
		wg.Go(func() {
			for job := range jobs {
				scanned.Add(1)
				clog.Debug().Path(field.Path, job.path).Msg("Scanning file")
				if file, ok := scanFile(job.path, job.size, cfg.maxSize, cfg.requireDirective); ok {
					mu.Lock()
					files = append(files, file)
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()

	if walkErr != nil {
		return nil, 0, walkErr
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return files, int(scanned.Load()), nil
}

// warnMissingRoots warns about any root that does not exist, so a typo'd path is
// visible rather than silently scanning nothing. The path is then left to the
// walk, which skips it - a missing root warns but does not fail the run.
func warnMissingRoots(roots []string) {
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			clog.Warn().Path(field.Path, root).Msg("Path does not exist")
		}
	}
}

// walk traverses root, sending scannable file paths to paths. Unreadable entries
// are skipped rather than aborting; VCS and ignored directories are pruned.
func walk(ctx context.Context, root string, cfg config, jobs chan<- scanJob) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			clog.Debug().
				Path(field.Path, path).
				Str(field.Reason, reasonUnreadable).
				Msg("Skipping path")
			return nil //nolint:nilerr // skip an unreadable entry, keep walking the rest
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			if vcsDirs[d.Name()] {
				clog.Debug().
					Path(field.Path, path).
					Str(field.Reason, reasonVCS).
					Msg("Skipping directory")
				return fs.SkipDir
			}
			if cfg.ignore(path, true) {
				clog.Debug().
					Path(field.Path, path).
					Str(field.Reason, reasonIgnored).
					Msg("Skipping directory")
				return fs.SkipDir
			}
			return nil
		}
		if cfg.ignore(path, false) {
			clog.Debug().
				Path(field.Path, path).
				Str(field.Reason, reasonIgnored).
				Msg("Skipping file")
			return nil
		}
		// Symlinks are never followed: a link could resolve outside the scanned
		// tree, letting a scan read - or the apply phase write through it - an
		// arbitrary path. Skip it rather than resolve it.
		if d.Type()&fs.ModeSymlink != 0 {
			clog.Debug().
				Path(field.Path, path).
				Str(field.Reason, reasonSymlink).
				Msg("Skipping symlink")
			return nil
		}
		info, err := d.Info()
		if err != nil {
			clog.Debug().
				Path(field.Path, path).
				Str(field.Reason, reasonUnreadable).
				Msg("Skipping file")
			return nil //nolint:nilerr // skip an unreadable entry, keep walking the rest
		}
		if !info.Mode().IsRegular() {
			clog.Debug().
				Path(field.Path, path).
				Str(field.Reason, reasonNonRegular).
				Msg("Skipping file")
			return nil
		}
		size := info.Size()

		select {
		case jobs <- scanJob{path: path, size: size}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
}
