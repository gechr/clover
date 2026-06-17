package vcs

import (
	"os"
	"path/filepath"
	"sync"
)

// vcsMarkers identify a repository root, one per supported VCS. A marker may be
// a directory (the usual case, and jj/hg/svn) or a file (.git in a submodule or
// linked worktree). A jj-colocated repo carries both .jj and .git in one
// directory, which still resolves to a single root.
var vcsMarkers = []string{".git", ".jj", ".hg", ".svn"}

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
// the nearest ancestor directory containing a VCS marker (.git, .jj, .hg, .svn)
// - or "" when the file is not inside a repository. The result is the namespace
// under which the file's id= is unique.
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
	case hasMarker(dir):
		root = dir
	default:
		if parent := filepath.Dir(dir); parent != dir {
			root = r.resolve(parent)
		}
	}

	r.cache[dir] = root
	return root
}

// hasMarker reports whether dir holds any VCS marker, as a file or directory.
func hasMarker(dir string) bool {
	for _, marker := range vcsMarkers {
		if _, err := os.Lstat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}
