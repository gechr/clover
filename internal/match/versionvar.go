package match

import (
	"regexp"
	"slices"

	"github.com/gechr/clover/internal/constant"
	xstrings "github.com/gechr/x/strings"
)

// versionVarName matches the name of a <TOOL>_VERSION variable, capturing the
// tool part. The name must be upper-case, the convention every shape this route
// covers follows (a Dockerfile ARG, a workflow env: value, a CI variable), which
// keeps an ordinary lower-case mapping key from being read as one.
var versionVarName = regexp.MustCompile(`\b([A-Z][A-Z0-9_]*)_VERSION\b`)

// nativeToolProviders maps the tool names Clover resolves with a provider of
// their own, including the aliases a variable is spelled with. They are absent
// from the generated mise registry map, which omits every tool the match package
// routes by hand, so a variable naming one is resolved here.
var nativeToolProviders = map[string]string{
	"go":     constant.ProviderGo,
	"golang": constant.ProviderGo,
	"node":   constant.ProviderNode,
	"nodejs": constant.ProviderNode,
	"python": constant.ProviderPython,
	"rust":   constant.ProviderRust,
	"swift":  constant.ProviderSwift,
	"zig":    constant.ProviderZig,
}

// inferVersionVariable reads the tool a <TOOL>_VERSION variable pins - a
// Dockerfile `ARG GO_VERSION=1.24.0`, a workflow `env:` value, a CI variable -
// resolving the name against the tool maps auto-detection already carries.
//
// The whole prefix must name a tool, never a trailing segment of it. That is the
// guard the shape lives or dies by, because the name is the only evidence:
// API_NODE_VERSION and INPUT_PYTHON_VERSION are a service's API version and a
// composite action's forwarded input, and reading the last segment would bump
// them as Node.js and Python. Requiring the whole prefix declines both, along
// with the API_VERSION, APP_VERSION, RELEASE_VERSION and CHART_VERSION family
// and every variable named for a project rather than a tool.
//
// An unrecognized prefix declines rather than guessing. A variable is named by a
// human for a human, so a name Clover cannot place is one it has no business
// resolving against an upstream that may have nothing to do with it.
//
// A variable paired with a sibling checksum variable also publishes an id, so
// that follower has a producer to name. It is set only for a real pairing, so an
// ordinary version variable still earns the bare `@clover` shorthand.
func inferVersionVariable(s subject) Inference {
	match := versionVarName.FindStringSubmatch(s.line())
	if match == nil {
		return Inference{}
	}
	inf := toolInference(xstrings.Slug(match[1]))
	if inf.Provider == "" {
		return inf
	}
	if pair, ok := pairChecksumVar(s); ok {
		inf.ID = pair.id
	}
	return inf
}

// toolInference resolves a tool name to the provider that tracks it and the
// parameter naming it there, in the same precedence the routes use: a native
// provider, then a HashiCorp product, then the GitHub repository a curated or
// registry tool releases from, then the ecosystem package a registry tool
// installs. It returns the zero Inference when the name places nowhere.
func toolInference(name string) Inference {
	if provider, ok := nativeToolProviders[name]; ok {
		return Inference{Provider: provider}
	}
	if slices.Contains(hashicorpProducts, name) {
		return Inference{Provider: constant.ProviderHashicorp, Product: name}
	}
	if repository, tagPrefix, ok := LookupTool(name); ok {
		return Inference{
			Provider:   constant.ProviderGithub,
			Repository: repository,
			TagPrefix:  tagPrefix,
		}
	}
	if pkg, provider := miseEcosystem(name); pkg != "" {
		return Inference{Provider: provider, Package: pkg}
	}
	return Inference{}
}
