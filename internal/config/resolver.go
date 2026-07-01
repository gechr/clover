package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/gechr/clover/internal/vcs"
)

// Resolver resolves the effective config governing a scanned path: the user
// config overlaid by the project .clover.yaml at the path's repository root (or,
// when the path is not in a repository, its own directory). Each root's config
// is loaded and merged once, then memoized, so a scan spanning many files across
// a handful of repositories does only a handful of reads. It also records the
// distinct roots it resolves, so a caller can tell a single-repository scan from
// a multi-repository one. Safe for concurrent use.
//
// Two flags short-circuit discovery: an explicit --config governs every path,
// and --no-config yields nil for all of them.
type Resolver struct {
	user     *Config
	explicit string
	disabled bool
	roots    *vcs.Resolver

	mu      sync.Mutex
	cache   map[string]result
	seen    map[string]struct{}
	loadErr error
}

// result memoizes one root's load outcome, error included, so a malformed config
// is not re-read on every file under it.
type result struct {
	cfg *Config
	err error
}

// NewResolver builds a per-root config resolver. user is the loaded user (XDG)
// config and may be nil. explicit is the --config path: when non-empty it is
// loaded for every path and root discovery is skipped. noConfig, the --no-config
// flag, makes every lookup nil for a fully unconfigured run.
func NewResolver(user *Config, explicit string, noConfig bool) *Resolver {
	return &Resolver{
		user:     user,
		explicit: explicit,
		disabled: noConfig,
		roots:    vcs.NewResolver(),
		cache:    make(map[string]result),
		seen:     make(map[string]struct{}),
	}
}

// Root returns the directory whose project config governs dir: the repository
// root containing dir, or dir itself (absolute) when it is not in a repository.
// It is the cache key ForDir resolves under, exposed so a caller can group files
// by the root that governs them.
func (r *Resolver) Root(dir string) string {
	if root := r.roots.RootDir(dir); root != "" {
		return root
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// ForDir returns the effective config governing the directory dir: the project
// config at dir's repository root (or dir itself when not in a repository),
// overlaid on the user config and memoized per root. With --config the explicit
// file governs every directory; with --no-config it returns nil. A malformed
// project config surfaces its load error (memoized, so it is read once).
func (r *Resolver) ForDir(dir string) (*Config, error) {
	switch {
	case r.disabled:
		return nil, nil //nolint:nilnil // no config requested
	case r.explicit != "":
		return r.explicitConfig()
	}

	root := r.Root(dir)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen[root] = struct{}{}
	if res, ok := r.cache[root]; ok {
		return res.cfg, res.err
	}
	res := loadRoot(r.user, root, "")
	r.cache[root] = res
	r.record(res.err)
	return res.cfg, res.err
}

// Primary returns the config to resolve per-invocation settings (output detail,
// fmt.prune) from: the explicit --config when set; else the sole root's config
// when the scan resolved to exactly one repository, so a single-tree run still
// honours its project config; else the user config, so a multi-repository scan
// falls back to the user default. It is meaningful only after the scan has
// driven ForDir over the scanned tree.
func (r *Resolver) Primary() *Config {
	switch {
	case r == nil, r.disabled:
		return nil
	case r.explicit != "":
		cfg, _ := r.explicitConfig()
		return cfg
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.seen) == 1 {
		for root := range r.seen {
			return r.cache[root].cfg
		}
	}
	return r.user
}

// PrimaryForPaths resolves the config for per-invocation settings that must be
// known before a scan starts. With one resolved root, that root's config wins;
// with multiple roots, the user config is the only unambiguous default.
func (r *Resolver) PrimaryForPaths(paths []string) (*Config, error) {
	switch {
	case r == nil, r.disabled:
		return nil, nil //nolint:nilnil // no config requested
	case r.explicit != "":
		return r.explicitConfig()
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}

	type seenConfig struct {
		cfg *Config
		err error
	}
	seen := make(map[string]seenConfig)
	for _, path := range paths {
		dir := configStartDir(path)
		root := r.Root(dir)
		cfg, err := r.ForDir(dir)
		if err != nil {
			return nil, err
		}
		seen[root] = seenConfig{cfg: cfg, err: err}
	}
	if len(seen) == 1 {
		for _, res := range seen {
			return res.cfg, res.err
		}
	}
	return r.user, nil
}

// explicitConfig loads and memoizes the --config file, overlaid on the user
// config. The same file governs every path, so it is keyed independently of any
// root.
func (r *Resolver) explicitConfig() (*Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res, ok := r.cache[""]; ok {
		return res.cfg, res.err
	}
	res := loadRoot(r.user, "", r.explicit)
	r.cache[""] = res
	r.record(res.err)
	return res.cfg, res.err
}

// record retains the first project-config load error encountered, so a walk that
// swallows per-directory errors (to keep scanning) can still fail the run once it
// finishes. The caller already holds the mutex.
func (r *Resolver) record(err error) {
	if err != nil && r.loadErr == nil {
		r.loadErr = err
	}
}

// Err returns the first project-config load error the resolver encountered while
// resolving paths, or nil. A malformed config is a hard error a caller surfaces
// after the scan, even when every file under the bad root was excluded or carried
// no directive.
func (r *Resolver) Err() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadErr
}

// loadRoot reads dir's project config (or the explicit path) and overlays it on
// user, packaging the outcome for the cache.
func loadRoot(user *Config, dir, path string) result {
	project, err := Load(dir, path)
	if err != nil {
		return result{err: err}
	}
	return result{cfg: Merge(user, project)}
}

// configStartDir returns the directory whose repository config should govern a
// scanned path. Existing files use their parent; directories and not-yet-existing
// paths use the path itself, matching scan's directory-root behavior.
func configStartDir(path string) string {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return filepath.Dir(path)
	}
	return path
}
