package match

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pattern"
	"github.com/gechr/clover/internal/version"
)

// Rewriter locates the version a target line carries. Implementations range from
// the shape-based [Smart] rewriter to format-specific ones. Locate is offline and
// pure, so lint runs it to validate a marker without resolving anything; the
// [Location] it returns renders the line once a candidate is resolved.
type Rewriter interface {
	// Locate extracts the version currently on the line, erroring when the
	// rewriter cannot act on it (no target, ambiguous, or malformed).
	Locate(line string) (Location, error)
}

// Location is what a Rewriter found on a target line: the common anchors the
// pipeline reads (Current, Semver, NeedsDigest) plus the ability to render itself
// for a resolved candidate. Each rewriter returns its own implementation, so the
// renderer-specific state (spans, captures) stays private to the rewriter that
// produced it rather than piling into a shared struct.
type Location interface {
	// Current is the version text currently on the line, recorded as the old value.
	Current() string
	// Semver is the parsed core of the current version, nil when unparseable. It
	// anchors selection.
	Semver() *version.Version
	// NeedsDigest reports whether rendering will rewrite a content digest, so the
	// pipeline knows to resolve one for the candidate.
	NeedsDigest() bool
	// Render rewrites the line for the resolved candidate, returning the new line
	// and whether it changed. It errors rather than reporting a silent no-op when
	// the candidate lacks a field it needs or the located span no longer fits.
	Render(line string, candidate model.Candidate) (string, bool, error)
}

// SecurePin is the optional capability of a [Location] whose target pins a secure
// value beside the version - an action commit SHA or an image content digest.
// Pinned reports the value currently on the line, so a run can cross-check it
// against the value the resolved tag reports and catch a committed pin that no
// longer matches upstream.
type SecurePin interface {
	Pinned() string
}

// Renderer is the optional capability of a [Location] that can report the exact
// version text it will write for a candidate, which may differ from the
// candidate's raw version once restyled (a stripped variant, a re-precisioned or
// re-prefixed core). The pipeline resolves a digest for this text rather than the
// raw candidate, so a pinned image's digest always describes the tag written. It
// also marks the target as restyling, so selection drops any candidate that is
// not version-shaped (see [Shaped]) - restyling cannot write one faithfully.
type Renderer interface {
	Rendered(candidate model.Candidate) string
}

// anchored carries the Current/Semver anchors every Location exposes; a concrete
// located embeds it, adds its own spans, and overrides Render (and NeedsDigest
// when it rewrites a digest).
type anchored struct {
	raw    string
	semver *version.Version
}

func (a anchored) Current() string          { return a.raw }
func (a anchored) Semver() *version.Version { return a.semver }
func (a anchored) NeedsDigest() bool        { return false }

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
	value     string
}

func (c conditions) match(ctx Context) bool {
	switch {
	case c.path != "" && !matchPath(c.path, ctx.Path):
		return false
	case c.lineMatch != nil && !c.lineMatch.Matches(ctx.Line):
		return false
	case c.provider != "" && c.provider != ctx.Provider:
		return false
	case c.value != "" && c.value != ctx.Value:
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
	when     conditions
	rewriter Rewriter
}

// miseGlob matches the mise configuration files whose [tools] entries pin the
// versions the mise routes recognize: mise.toml and .mise.toml, their .local
// and environment variants (mise.local.toml, mise.<env>.toml), the directory
// forms (mise/config.toml, .mise/config.toml, .config/mise/config.toml,
// .config/mise.toml), and .config/mise/conf.d fragments.
const miseGlob = "**/{{.mise,mise}{,.*}.toml,{.mise,mise}/config.toml,.config/mise/conf.d/*.toml}"

// toolVersionsGlob matches asdf's .tool-versions files, which mise reads with
// the same tool names as its own configuration.
const toolVersionsGlob = "**/.tool-versions"

// dockerfileGlob matches both the prefix naming convention (Dockerfile,
// Dockerfile.dev, Containerfile) and the suffix convention (app.Dockerfile).
const dockerfileGlob = "**/{Dockerfile,Containerfile,*.Dockerfile,*.Containerfile}*"

// MiseFile reports whether path is a file mise reads tool pins from (its own
// configuration or asdf's .tool-versions), where a bare single-number pin
// (node = "24") means major precision rather than a calendar tag, so selection
// may relax its scheme guard.
func MiseFile(path string) bool {
	return matchPath(miseGlob, path) || matchPath(toolVersionsGlob, path)
}

// goModGlob matches Go module files, whose go directive pins the toolchain.
const goModGlob = "**/go.mod"

// pyprojectGlob matches Python project files, whose target-version key pins the
// interpreter a tool targets.
const pyprojectGlob = "**/pyproject.toml"

// terraformGlob matches Terraform configuration files, whose required_version
// constraint pins the toolchain.
const terraformGlob = "**/*.tf"

// tofuGlob matches OpenTofu configuration files. The .tofu extension is the
// one unambiguous OpenTofu signal, since .tf contents are identical for both
// toolchains.
const tofuGlob = "**/*.tofu"

// hashicorpProducts are the mise tool names that double as HashiCorp product
// slugs on releases.hashicorp.com, alternated into the mise route's pattern.
var hashicorpProducts = []string{
	"boundary",
	"consul",
	"nomad",
	"packer",
	"terraform",
	"vagrant",
	"vault",
	"waypoint",
}

// routes is the ordered, first-match-wins dispatch table. Smart is the
// empty-condition catch-all and must stay last. It is a curated built-in list,
// not user configuration (yet).
var routes = []route{
	{
		// A digest-pinned workflow container job: uses: docker://img:tag@sha256:… .
		// The docker:// scheme marks the reference as an image, so it must precede
		// the action uses: routes.
		when: conditions{
			path: "**/*.{yml,yaml}",
			lineMatch: mustPattern(
				"* uses: *" + dockerScheme + "*" + constant.DockerDigestMarker + "*",
			),
			provider: constant.ProviderDocker,
		},
		rewriter: NewDockerPin(),
	},
	{
		// A tag-only workflow container job: uses: docker://img:tag.
		when: conditions{
			path:      "**/*.{yml,yaml}",
			lineMatch: mustPattern("* uses: *" + dockerScheme + "*"),
			provider:  constant.ProviderDocker,
		},
		rewriter: NewDockerTag(),
	},
	{
		// A SHA-pinned GitHub Actions reference: uses: owner/repo@<40-hex> # vX.Y.Z,
		// updated SHA + version comment in lockstep. Scoped to YAML, the only place a
		// real uses: lives, so annotate never fires on an example in Markdown prose.
		// The whitespace around uses: anchors it to a list key, not a substring like
		// reuses:.
		when: conditions{
			path:      "**/*.{yml,yaml}",
			lineMatch: mustPattern(`/\s+uses:\s+.+@[0-9a-fA-F]{40}\b/`),
			provider:  constant.ProviderGithub,
		},
		rewriter: NewActionPin(),
	},
	{
		// A tag-pinned GitHub Actions reference: uses: owner/repo@vX.Y.Z. The
		// action-tag rewriter converts it to the secure pin format (@<sha> with a
		// # version comment) rather than bumping the tag in place - clover is
		// secure by default. Must follow the SHA-pinned route, which claims the
		// pins whose version lives in the trailing comment.
		when: conditions{
			path:      "**/*.{yml,yaml}",
			lineMatch: mustPattern(`/\s+uses:\s+\S+@v?\d/`),
			provider:  constant.ProviderGithub,
		},
		rewriter: NewActionTag(),
	},
	{
		// A digest-pinned Dockerfile FROM; the @sha256 makes it a secure pin,
		// so the docker-pin rewriter updates tag and digest together. Must
		// precede the tag-only FROM route.
		when: conditions{
			path:      dockerfileGlob,
			lineMatch: mustPattern("FROM *" + constant.DockerDigestMarker + "*"),
			provider:  constant.ProviderDocker,
		},
		rewriter: NewDockerPin(),
	},
	{
		// A digest-pinned compose/Kubernetes image: mapping.
		when: conditions{
			path:      "**/*.{yml,yaml}",
			lineMatch: mustPattern("* image: *" + constant.DockerDigestMarker + "*"),
			provider:  constant.ProviderDocker,
		},
		rewriter: NewDockerPin(),
	},
	{
		// A tag-only Dockerfile FROM instruction; the docker-tag rewriter
		// anchors on the image reference so a registry :port or account id is
		// never mistaken for the version.
		when: conditions{
			path:      dockerfileGlob,
			lineMatch: mustPattern("FROM *"),
			provider:  constant.ProviderDocker,
		},
		rewriter: NewDockerTag(),
	},
	{
		// A tag-only compose/Kubernetes image: mapping. The leading and
		// trailing spaces in the pattern avoid matching keys like customimage:.
		when: conditions{
			path:      "**/*.{yml,yaml}",
			lineMatch: mustPattern("* image: *"),
			provider:  constant.ProviderDocker,
		},
		rewriter: NewDockerTag(),
	},
	{
		// A GitLab CI/CD component include: component: <host>/<project>/<name>@<version>.
		// The version is the only version-shaped token on the line, so the smart
		// rewriter bumps it in place.
		when: conditions{
			path:      "**/*.{yml,yaml}",
			lineMatch: mustPattern("* component: *@*"),
			provider:  constant.ProviderGitlab,
		},
		rewriter: NewSmart(),
	},
	{
		// A mise tool pinning a HashiCorp product: terraform = "1.9.8". The tool
		// name doubles as the product slug on releases.hashicorp.com.
		when: conditions{
			path: miseGlob,
			lineMatch: mustPattern(
				`/^\s*"?(` + strings.Join(hashicorpProducts, "|") + `)"?\s*=\s*"/`,
			),
			provider: constant.ProviderHashicorp,
		},
		rewriter: NewSmart(),
	},
	{
		// The same HashiCorp product pinned in .tool-versions: terraform 1.9.8.
		when: conditions{
			path: toolVersionsGlob,
			lineMatch: mustPattern(
				`/^\s*(` + strings.Join(hashicorpProducts, "|") + `)\s+\S/`,
			),
			provider: constant.ProviderHashicorp,
		},
		rewriter: NewSmart(),
	},
	{
		// A mise Node.js runtime pin: node = "24.18.0". nodejs is mise's builtin
		// alias for the asdf plugin name.
		when: conditions{
			path:      miseGlob,
			lineMatch: mustPattern(`/^\s*"?(node|nodejs)"?\s*=\s*"/`),
			provider:  constant.ProviderNode,
		},
		rewriter: NewSmart(),
	},
	{
		// The same runtime pinned in .tool-versions: nodejs 24.18.0.
		when: conditions{
			path:      toolVersionsGlob,
			lineMatch: mustPattern(`/^\s*(node|nodejs)\s+\S/`),
			provider:  constant.ProviderNode,
		},
		rewriter: NewSmart(),
	},
	{
		// A mise Go toolchain pin: go = "1.26.5". The go provider resolves it
		// from go.dev, so this precedes the general GitHub-tool route that would
		// otherwise claim it. golang is mise's builtin alias for the asdf plugin
		// name.
		when: conditions{
			path:      miseGlob,
			lineMatch: mustPattern(`/^\s*"?(go|golang)"?\s*=\s*"/`),
			provider:  constant.ProviderGo,
		},
		rewriter: NewSmart(),
	},
	{
		// The same toolchain pinned in .tool-versions: golang 1.26.5.
		when: conditions{
			path:      toolVersionsGlob,
			lineMatch: mustPattern(`/^\s*(go|golang)\s+\S/`),
			provider:  constant.ProviderGo,
		},
		rewriter: NewSmart(),
	},
	{
		// A mise Python runtime pin: python = "3.14.5". The python provider
		// resolves it from python.org, so this precedes the general GitHub-tool
		// route.
		when: conditions{
			path:      miseGlob,
			lineMatch: mustPattern(`/^\s*"?python"?\s*=\s*"/`),
			provider:  constant.ProviderPython,
		},
		rewriter: NewSmart(),
	},
	{
		// The same runtime pinned in .tool-versions: python 3.14.5.
		when: conditions{
			path:      toolVersionsGlob,
			lineMatch: mustPattern(`/^\s*python\s+\S/`),
			provider:  constant.ProviderPython,
		},
		rewriter: NewSmart(),
	},
	{
		// A mise Zig toolchain pin: zig = "0.15.2". The zig provider resolves it
		// from ziglang.org, so this precedes the general GitHub-tool route.
		when: conditions{
			path:      miseGlob,
			lineMatch: mustPattern(`/^\s*"?zig"?\s*=\s*"/`),
			provider:  constant.ProviderZig,
		},
		rewriter: NewSmart(),
	},
	{
		// The same toolchain pinned in .tool-versions: zig 0.15.2.
		when: conditions{
			path:      toolVersionsGlob,
			lineMatch: mustPattern(`/^\s*zig\s+\S/`),
			provider:  constant.ProviderZig,
		},
		rewriter: NewSmart(),
	},
	{
		// A compact Python target in pyproject.toml: target-version = "py314"
		// (ruff, black, mypy). The pyXY form is not version-shaped, so a dedicated
		// rewriter parses and re-renders it. requires-python is deliberately not
		// auto-detected: it declares a minimum supported version, not the
		// interpreter to track to latest, and its common ">=3.10,<4" range carries
		// two version tokens the smart rewriter cannot disambiguate.
		when: conditions{
			path:      pyprojectGlob,
			lineMatch: mustPattern(`/target-version\s*=\s*['"]py\d/`),
			provider:  constant.ProviderPython,
		},
		rewriter: NewPythonTag(),
	},
	{
		// A mise github: or ubi: backend tool: "github:owner/repo" = "v1.2.3".
		// Both back onto GitHub releases, so the github provider tracks them.
		when: conditions{
			path:      miseGlob,
			lineMatch: mustPattern(`/^\s*"(github|ubi):[^"]+"\s*=\s*"/`),
			provider:  constant.ProviderGithub,
		},
		rewriter: NewSmart(),
	},
	{
		// The same backend tool pinned in .tool-versions, unquoted:
		// ubi:owner/repo 1.2.3.
		when: conditions{
			path:      toolVersionsGlob,
			lineMatch: mustPattern(`/^\s*(github|ubi):\S+\s+\S/`),
			provider:  constant.ProviderGithub,
		},
		rewriter: NewSmart(),
	},
	{
		// A well-known mise tool released on GitHub: tofu = "1.8.5" tracks
		// opentofu/opentofu. The names come from the curated miseGithubTools map
		// and the generated mise registry map.
		when: conditions{
			path:      miseGlob,
			lineMatch: mustPattern(`/^\s*"?(` + miseToolAlternation() + `)"?\s*=\s*"/`),
			provider:  constant.ProviderGithub,
		},
		rewriter: NewSmart(),
	},
	{
		// The same well-known tool pinned in .tool-versions: tofu 1.8.5.
		when: conditions{
			path:      toolVersionsGlob,
			lineMatch: mustPattern(`/^\s*(` + miseToolAlternation() + `)\s+\S/`),
			provider:  constant.ProviderGithub,
		},
		rewriter: NewSmart(),
	},
	{
		// A Terraform required_version constraint: required_version = "~> 1.11.0".
		// The version inside the constraint string is the only version-shaped
		// token, so the smart rewriter bumps it in place, preserving the operator
		// and precision around it.
		when: conditions{
			path:      terraformGlob,
			lineMatch: mustPattern(`/^\s*required_version\s*=\s*"/`),
			provider:  constant.ProviderHashicorp,
		},
		rewriter: NewSmart(),
	},
	{
		// A required_providers version constraint: version = "~> 6.39". The
		// source address lives on a sibling line of the entry, so inference
		// parses the enclosing block (see terraformSource); a version attribute
		// outside required_providers infers no source and is skipped.
		when: conditions{
			path:      terraformGlob,
			lineMatch: mustPattern(`/^\s*version\s*=\s*"/`),
			provider:  constant.ProviderTerraform,
		},
		rewriter: NewSmart(),
	},
	{
		// An OpenTofu required_version constraint in a .tofu file. OpenTofu
		// releases live on GitHub, not releases.hashicorp.com, so the github
		// provider tracks them.
		when: conditions{
			path:      tofuGlob,
			lineMatch: mustPattern(`/^\s*required_version\s*=\s*"/`),
			provider:  constant.ProviderGithub,
		},
		rewriter: NewSmart(),
	},
	{
		// A required_providers version constraint in a .tofu file resolves
		// against the OpenTofu registry.
		when: conditions{
			path:      tofuGlob,
			lineMatch: mustPattern(`/^\s*version\s*=\s*"/`),
			provider:  constant.ProviderOpentofu,
		},
		rewriter: NewSmart(),
	},
	{
		// The go directive in go.mod: go 1.23.2. The go provider resolves the
		// toolchain from the go.dev download index, stripping the goX.Y.Z prefix
		// to clean semver.
		when: conditions{
			path:      goModGlob,
			lineMatch: mustPattern(`/^go\s+\d/`),
			provider:  constant.ProviderGo,
		},
		rewriter: NewSmart(),
	},
	{
		// A follower projecting a commit or sha256 onto its own line; the hash
		// rewriter swaps the existing hex token. Followers carry provider=follow,
		// so this never collides with the provider-gated routes above.
		when:     conditions{value: constant.ValueCommit},
		rewriter: NewHash(),
	},
	{
		when:     conditions{value: constant.ValueSha256},
		rewriter: NewHash(),
	},
	{rewriter: NewSmart()},
}

// miseToolAlternation joins every GitHub-released mise tool name - curated and
// generated - into the well-known-tool route's regex alternation, each name
// quoted so a dot in a tool name stays literal.
func miseToolAlternation() string {
	names := ToolNames()
	for i, name := range names {
		names[i] = regexp.QuoteMeta(name)
	}
	return strings.Join(names, "|")
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
			return r.rewriter
		}
	}
	return NewSmart()
}
