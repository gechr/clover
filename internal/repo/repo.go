package repo

import (
	"os"
	"path/filepath"
	"sync"
)

// gitEntry is the marker that identifies a repository root. It may be a
// directory (normal clone) or a file (submodule or linked worktree).
const gitEntry = ".git"

// Resolver maps a file to its git repository root, caching each directory it
// resolves so an ancestor is checked at most once. Safe for concurrent use by
// the scan workers.
type Resolver struct {
	mu    sync.Mutex
	cache map[string]string
}

// NewResolver returns an empty resolver.
func NewResolver() *Resolver {
	return &Resolver{cache: make(map[string]string)}
}

// Root returns the absolute path of the repository the file at path belongs to -
// the nearest ancestor directory containing a .git entry - or "" when the file
// is not inside a repository. The result is the namespace under which the file's
// id= is unique.
func (r *Resolver) Root(path string) string {
	dir := path
	if abs, err := filepath.Abs(path); err == nil {
		dir = abs
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resolve(filepath.Dir(dir))
}

// resolve walks up from dir to find the repository root. The caller holds r.mu;
// recursion is memoised so each directory is statted once across all lookups.
func (r *Resolver) resolve(dir string) string {
	if cached, ok := r.cache[dir]; ok {
		return cached
	}

	var root string
	switch {
	case exists(filepath.Join(dir, gitEntry)):
		root = dir
	default:
		if parent := filepath.Dir(dir); parent != dir {
			root = r.resolve(parent)
		}
	}

	r.cache[dir] = root
	return root
}

// exists reports whether a filesystem entry is present, following the same
// semantics for a .git directory or file.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
