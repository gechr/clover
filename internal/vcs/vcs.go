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
	cwd string

	mu    sync.Mutex
	cache map[string]string
}

// NewResolver returns an empty resolver. It captures the working directory
// once - a scan hands it relative paths, and filepath.Abs would otherwise
// re-issue the getwd syscall for every lookup of a value that never changes
// during a run.
func NewResolver() *Resolver {
	cwd, _ := os.Getwd()
	return &Resolver{cwd: cwd, cache: make(map[string]string)}
}

// Root returns the absolute path of the repository the file at path belongs to -
// the nearest ancestor directory containing a VCS marker (.git, .jj, .hg, .svn)
// - or "" when the file is not inside a repository. The result is the namespace
// under which the file's id= is unique.
func (r *Resolver) Root(path string) string {
	return r.RootDir(filepath.Dir(r.abs(path)))
}

// RootDir returns the absolute path of the repository containing the directory
// dir - the nearest ancestor, dir itself included, holding a VCS marker - or ""
// when dir is not inside a repository. Unlike Root, dir is the search start
// rather than a file whose parent is searched, so a scanned directory anchors on
// its own repository root.
func (r *Resolver) RootDir(dir string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resolve(r.abs(dir))
}

// abs returns path made absolute against the captured working directory, or
// path unchanged when that is not possible.
func (r *Resolver) abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if r.cwd != "" {
		return filepath.Join(r.cwd, path)
	}
	if a, err := filepath.Abs(path); err == nil {
		return a
	}
	return path
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
