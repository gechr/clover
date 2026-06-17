package scan

import (
	"context"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// defaultMaxSize caps the file size scan will read, so a stray large file never
// stalls a run.
const defaultMaxSize = 5 << 20 // 5 MiB

// vcsDirs are always skipped during the walk.
var vcsDirs = map[string]bool{".git": true, ".jj": true, ".hg": true, ".svn": true}

type config struct {
	workers int
	maxSize int64
	ignore  func(path string, isDir bool) bool
}

// Option configures [Scan].
type Option func(*config)

// WithWorkers sets the number of files scanned concurrently (default: NumCPU).
func WithWorkers(n int) Option { return func(c *config) { c.workers = n } }

// WithMaxSize sets the largest file scan will read.
func WithMaxSize(n int64) Option { return func(c *config) { c.maxSize = n } }

// WithIgnore supplies the predicate that skips ignored files and directories -
// the seam a gitignore matcher plugs into. It is consulted in addition to the
// always-skipped VCS directories.
func WithIgnore(fn func(path string, isDir bool) bool) Option {
	return func(c *config) { c.ignore = fn }
}

// Scan walks roots and returns the files carrying a cusp: directive, sorted by
// path for deterministic output. A single walker produces paths that a pool of
// workers reads and scans concurrently.
func Scan(ctx context.Context, roots []string, opts ...Option) ([]File, error) {
	cfg := config{
		workers: runtime.NumCPU(),
		maxSize: defaultMaxSize,
		ignore:  func(string, bool) bool { return false },
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.workers < 1 {
		cfg.workers = 1
	}

	paths := make(chan string)
	var walkErr error
	go func() {
		defer close(paths)
		for _, root := range roots {
			if err := walk(ctx, root, cfg, paths); err != nil {
				walkErr = err
				return
			}
		}
	}()

	var (
		mu    sync.Mutex
		files []File
		wg    sync.WaitGroup
	)
	for range cfg.workers {
		wg.Go(func() {
			for path := range paths {
				if file, ok := scanFile(path, cfg.maxSize); ok {
					mu.Lock()
					files = append(files, file)
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()

	if walkErr != nil {
		return nil, walkErr
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// walk traverses root, sending scannable file paths to paths. Unreadable entries
// are skipped rather than aborting; VCS and ignored directories are pruned.
func walk(ctx context.Context, root string, cfg config, paths chan<- string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip an unreadable entry, keep walking the rest
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			if vcsDirs[d.Name()] || cfg.ignore(path, true) {
				return fs.SkipDir
			}
			return nil
		}
		if cfg.ignore(path, false) {
			return nil
		}

		select {
		case paths <- path:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
}
