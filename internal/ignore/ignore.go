package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gechr/clover/internal/vcs"
)

// defaultFiles are the per-directory ignore files clover reads, lowest priority
// first. All use gitignore syntax. A future .cloverignore (or .ignore) is added by
// extending this list via [WithFiles]; later names override earlier ones.
var defaultFiles = []string{".gitignore"}

// Matcher decides whether a path is ignored by the ignore files governing it,
// after the manner of ripgrep's ignore handling but owned in-repo. It is bounded
// by the repository root (resolved via vcs) and caches each directory's parsed
// patterns, so it is cheap to consult once per scanned entry.
type Matcher struct {
	resolver *vcs.Resolver
	files    []string
	disabled bool

	mu    sync.Mutex
	cache map[string][]pattern
}

// Option configures a [Matcher].
type Option func(*Matcher)

// WithFiles sets the ignore file names read in each directory, lowest priority
// first (default: .gitignore). This is the seam for a future .cloverignore.
func WithFiles(names ...string) Option {
	return func(m *Matcher) { m.files = names }
}

// WithDisabled turns the matcher into a no-op that ignores nothing, for
// --no-ignore. VCS directories are pruned by the walker, not here, so they stay
// excluded regardless.
func WithDisabled() Option {
	return func(m *Matcher) { m.disabled = true }
}

// New returns a matcher that resolves repository roots through resolver.
func New(resolver *vcs.Resolver, opts ...Option) *Matcher {
	m := &Matcher{resolver: resolver, files: defaultFiles, cache: make(map[string][]pattern)}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Ignore reports whether the entry at path is ignored, suitable for
// scan.WithIgnore. Files outside any repository are never ignored. The ignore
// files from the repository root down to the path's directory are applied in
// order, the last matching pattern deciding - matching git, including negation.
func (m *Matcher) Ignore(path string, isDir bool) bool {
	if m.disabled {
		return false
	}
	root := m.resolver.Root(path)
	if root == "" {
		return false
	}

	abs := path
	if a, err := filepath.Abs(path); err == nil {
		abs = a
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
		return false
	}

	ignored := m.apply(root, rel, isDir, false)
	dir := root
	parts := strings.Split(rel, "/")
	for i := range len(parts) - 1 {
		dir = filepath.Join(dir, parts[i])
		ignored = m.apply(dir, strings.Join(parts[i+1:], "/"), isDir, ignored)
	}
	return ignored
}

// apply runs the patterns from dir's ignore files against rel (the path relative
// to dir), updating ignored on each match so the last match wins.
func (m *Matcher) apply(dir, rel string, isDir, ignored bool) bool {
	for _, p := range m.load(dir) {
		if p.match(rel, isDir) {
			ignored = !p.negated
		}
	}
	return ignored
}

// load returns dir's parsed ignore patterns, reading and caching on first use. A
// directory's configured ignore files are concatenated lowest priority first, so
// a later file's patterns override an earlier file's; a directory with none
// caches as empty.
func (m *Matcher) load(dir string) []pattern {
	m.mu.Lock()
	defer m.mu.Unlock()
	if patterns, ok := m.cache[dir]; ok {
		return patterns
	}

	var patterns []pattern
	for _, name := range m.files {
		if content, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			patterns = append(patterns, parse(string(content))...)
		}
	}
	m.cache[dir] = patterns
	return patterns
}
