package match

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pattern"
	"github.com/gechr/clover/internal/version"
)

// Rewriter locates the version a target line carries and rewrites the line for a
// resolved candidate. Implementations range from the shape-based [Smart]
// rewriter to format-specific ones. Both methods are offline and pure: Locate is
// what lint runs to validate a marker; the located result is handed back to
// Render so style and spans are fixed once, never re-derived.
type Rewriter interface {
	// Locate extracts the version currently on the line, erroring when the
	// rewriter cannot act on it (no target, ambiguous, or malformed).
	Locate(line string) (Located, error)
	// Render rewrites the line for the resolved candidate using the prior Locate
	// result, returning the new line and whether it changed. It errors rather than
	// reporting a silent no-op when the candidate lacks a field it needs or the
	// located span no longer fits the line.
	Render(line string, located Located, candidate model.Candidate) (string, bool, error)
}

// Located is what a Rewriter found on a target line. Raw (the current version
// text) and Semver (its parsed core, nil when unparseable) are the public
// anchors the pipeline reads - Semver anchors selection, Raw records the old
// value. The remaining fields are private state the locating rewriter hands to
// its own Render, so spans and style are fixed at Locate time.
type Located struct {
	Raw    string
	Semver *version.Version

	token  Token // the version token, with span and style
	commit Span  // action-pin: the commit SHA span (zero for smart)
}

// Context is what the dispatch table routes on: the file, the target line, the
// marker's provider, and the follower value kind.
type Context struct {
	Path     string
	Line     string
	Provider string
	Value    string
}

// conditions guards a route; every set field must match (AND). path uses a
// doublestar (**-aware) glob - the right dialect for file paths, where **
// spans directories - while lineMatch reuses the token pattern engine for the
// target line's content. Both are optional; an empty field matches any.
type conditions struct {
	path      string
	lineMatch *pattern.Pattern
	provider  string
}

func (c conditions) match(ctx Context) bool {
	switch {
	case c.path != "" && !matchPath(c.path, ctx.Path):
		return false
	case c.lineMatch != nil && !c.lineMatch.Matches(ctx.Line):
		return false
	case c.provider != "" && c.provider != ctx.Provider:
		return false
	default:
		return true
	}
}

// matchPath reports whether path matches the doublestar glob. A malformed glob
// (only a programmer error for the built-in routes) never matches.
func matchPath(glob, path string) bool {
	ok, err := doublestar.Match(glob, path)
	return err == nil && ok
}

// route pairs a guard with the rewriter to use when it matches.
type route struct {
	when conditions
	rw   Rewriter
}

// routes is the ordered, first-match-wins dispatch table. Smart is the
// empty-condition catch-all and must stay last. It is a curated built-in list,
// not user configuration (yet).
var routes = []route{
	{
		when: conditions{
			// ** spans any leading directories, so this matches a workflow file
			// whether the scan root is the repo, an absolute path, or a sub-dir,
			// while .github stays a real path segment (not hello.github).
			path:      "**/.github/workflows/*.{yml,yaml}",
			lineMatch: mustPattern("* uses: *"),
			provider:  constant.ProviderGithub,
		},
		rw: NewActionPin(),
	},
	{
		// A Dockerfile FROM instruction; the smart rewriter handles the
		// name:tag span, so this route exists mainly to name docker for
		// provider=auto. A digest-pin rewriter would slot in here later.
		when: conditions{
			path:      "**/{Dockerfile,Containerfile}*",
			lineMatch: mustPattern("FROM *"),
			provider:  constant.ProviderDocker,
		},
		rw: NewSmart(),
	},
	{
		// A compose/Kubernetes image: mapping. The leading and trailing spaces
		// in the pattern avoid matching keys like customimage:.
		when: conditions{
			path:      "**/*.{yml,yaml}",
			lineMatch: mustPattern("* image: *"),
			provider:  constant.ProviderDocker,
		},
		rw: NewSmart(),
	},
	{rw: NewSmart()},
}

// mustPattern compiles a built-in route pattern, panicking on a malformed one
// since the patterns are constant literals that cannot fail at runtime.
func mustPattern(raw string) *pattern.Pattern {
	p, err := pattern.Compile(raw)
	if err != nil {
		panic(fmt.Sprintf("match: invalid built-in route pattern %q: %v", raw, err))
	}
	return p
}

// For selects the rewriter for a target line, walking the routes first-match-wins
// and falling back to the smart rewriter.
func For(ctx Context) Rewriter {
	for _, r := range routes {
		if r.when.match(ctx) {
			return r.rw
		}
	}
	return NewSmart()
}
