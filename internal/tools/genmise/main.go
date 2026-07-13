// Command genmise generates the maps the match package's auto-detection uses to
// resolve a mise tool name to the upstream whose versions it tracks. It reads
// the registry directory of a mise checkout (github.com/jdx/mise) and records,
// for each tool, the provider its backends prove carry the pinned versions.
//
// A tool's backends are interchangeable installers for the same pinned version.
// A GitHub-shaped backend - aqua:, github:, or ubi:, in any position - is
// preferred, since GitHub tags are the most universal version source, and the
// tool maps to that owner/repo under the github provider. A tool with no GitHub
// backend falls back to its first package-manager backend: pipx: to pypi, npm:
// to npm, cargo: to crates, mapping to the package that ecosystem installs.
// Tools backed only by an unsupported ecosystem (core:, asdf:, gem:, ...) are
// skipped, as are tools the match package already maps by hand (HashiCorp
// products, the Go toolchain, the Node.js runtime), so the curated mappings
// stay authoritative.
//
// The output declares one map per provider: miseRegistryTools (github, name to
// owner/repo), misePypiTools, miseNpmTools, and miseCratesTools (name to
// package).
//
// Usage:
//
//	go run ./internal/tools/genmise [-src <mise-checkout>] [-ref <branch>] [-o <file>]
//
// Without -src the mise repository is shallow-cloned to a temporary directory
// at the -ref branch (main by default), and the header records the commit the
// map was generated from.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gechr/clover/internal/constant"
	xmaps "github.com/gechr/x/maps"
)

// miseRepo is the upstream repository a depth-1 clone fetches the registry from.
const miseRepo = "https://github.com/jdx/mise"

// registryTool is the subset of a mise registry entry the generator reads.
// Backends decodes loosely: an entry is usually a plain "scheme:spec" string,
// but may be a table ({ full = "...", options = ... }) whose full key carries
// the same spec.
type registryTool struct {
	Backends []any    `toml:"backends"`
	Aliases  []string `toml:"aliases"`
}

// entry is a resolved registry tool: the Clover provider whose ecosystem
// carries the tool's versions, and the provider-specific spec the map records -
// a GitHub owner/repo, or a package name.
type entry struct {
	provider string
	spec     string
}

// backendSpec returns a backend entry's "scheme:spec" string, "" when the
// entry carries none.
func backendSpec(backend any) string {
	switch b := backend.(type) {
	case string:
		return b
	case map[string]any:
		full, _ := b["full"].(string)
		return full
	default:
		return ""
	}
}

// githubBackends are the mise backend schemes whose spec is a GitHub
// owner/repo whose tags carry the tool's versions.
var githubBackends = []string{"aqua:", "github:", "ubi:"}

// ecosystemBackends maps a mise package-manager backend scheme to the Clover
// provider whose registry tracks the same versions. A tool with no GitHub
// backend falls back to its first backend named here, so a pipx:/npm:/cargo:
// -only tool resolves to pypi/npm/crates rather than being dropped.
var ecosystemBackends = map[string]string{
	"pipx:":  constant.ProviderPypi,
	"npm:":   constant.ProviderNpm,
	"cargo:": constant.ProviderCrates,
}

// curated are the tool names the match package maps by hand; the generator
// never emits them so the curated entries stay authoritative.
var curated = []string{
	// HashiCorp products, mapped to the hashicorp provider.
	"boundary", "consul", "nomad", "packer", "terraform", "vagrant", "vault", "waypoint",
	// Mapped by hand in the match package.
	"bun", "deno", "elixir", "erlang", "go", "node", "opentofu", "python", "rust", "swift",
	"tofu", "zig",
}

// owner validates the owner segment of an owner/repo spec. A GitHub owner is
// alphanumerics and hyphens only, so an aqua HTTP-type package whose first
// segment is a domain (atlassian.com/acli) is rejected.
var owner = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// segment validates the repository segment (and an alias name), where dots and
// underscores are legal. It also validates a pypi or crate package name, which
// carries no path separator.
var segment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// npmPackage validates an npm package name, optionally @scope/-prefixed, so a
// scoped spec (npm:@antfu/ni) is kept while a git URL or other exotic spec is
// rejected.
var npmPackage = regexp.MustCompile(`^(@[A-Za-z0-9._-]+/)?[A-Za-z0-9._-]+$`)

func main() {
	src := flag.String("src", "", "path to a mise checkout (cloned to a temp dir when omitted)")
	ref := flag.String("ref", "main", "mise ref to generate from, recorded in the header")
	out := flag.String("o", "zz_generated.miseregistry.go", "output file")
	flag.Parse()
	log.SetFlags(0)

	if err := run(context.Background(), *src, *ref, *out); err != nil {
		log.Fatal(err)
	}
}

// run generates the map from the checkout at src (cloning one when empty) and
// writes it to out. It is main's body, split out so its defers run before the
// fatal-exit error handling.
func run(ctx context.Context, src, ref, out string) error {
	if src == "" {
		clone, cleanup, err := cloneMise(ctx, ref)
		if err != nil {
			return err
		}
		defer cleanup()
		src = clone
	}

	tools, err := read(filepath.Join(src, "registry"))
	if err != nil {
		return err
	}
	source, err := render(tools, describe(ctx, src, ref))
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(out); err == nil && bytes.Equal(body(old), body(source)) {
		log.Printf("skipped %s: only the mise ref changed (%d tools)", out, len(tools))
		return nil
	}
	if err := os.WriteFile(out, source, 0o600); err != nil {
		return err
	}
	log.Printf("wrote %d tools to %s", len(tools), out)
	return nil
}

// body returns the source with its header comment line dropped, so a rewrite
// that only bumped the recorded mise ref compares equal to the file on disk.
func body(source []byte) []byte {
	_, rest, _ := bytes.Cut(source, []byte("\n"))
	return rest
}

// cloneMise fetches the mise repository at ref into a temporary directory with
// a depth-1 clone, returning the checkout path and its cleanup.
func cloneMise(ctx context.Context, ref string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "genmise-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	cmd := exec.CommandContext(
		ctx,
		"git",
		"clone",
		"--quiet",
		"--depth",
		"1",
		"--branch",
		ref,
		miseRepo,
		dir,
	)
	cmd.Stderr = os.Stderr
	log.Printf("cloning %s (%s)", miseRepo, ref)
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("clone mise: %w", err)
	}
	return dir, cleanup, nil
}

// describe returns the ref decorated with the checkout's commit (main@abc1234)
// so the generated header pins what a moving branch pointed at, falling back to
// the bare ref when src is not a git checkout.
func describe(ctx context.Context, src, ref string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", src, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ref
	}
	return ref + "@" + strings.TrimSpace(string(out))
}

// read parses every registry TOML under dir and returns the tool-name (and
// alias) to resolved-entry map.
func read(dir string) (map[string]entry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no registry files under %q", dir)
	}

	tools := map[string]entry{}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".toml")
		if slices.Contains(curated, name) {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var tool registryTool
		if err := toml.Unmarshal(data, &tool); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		resolved, ok := resolve(tool.Backends)
		if !ok {
			continue
		}
		tools[name] = resolved
		for _, alias := range tool.Aliases {
			if segment.MatchString(alias) && !slices.Contains(curated, alias) {
				tools[alias] = resolved
			}
		}
	}
	return tools, nil
}

// resolve returns the provider and spec the tool's backends prove carry its
// versions: a GitHub owner/repo from the first github-shaped backend (preferred
// regardless of list position, since GitHub tags are the most universal version
// source), else the package name of the first pipx:/npm:/cargo: backend. ok is
// false when no backend maps to a supported provider.
func resolve(backends []any) (entry, bool) {
	if repo, ok := repository(backends); ok {
		return entry{provider: constant.ProviderGithub, spec: repo}, true
	}
	for _, backend := range backends {
		spec := backendSpec(backend)
		for scheme, provider := range ecosystemBackends {
			pkg, ok := strings.CutPrefix(spec, scheme)
			if !ok {
				continue
			}
			pkg, _, _ = strings.Cut(pkg, "[") // drop a trailing [option] qualifier
			if packageName(provider).MatchString(pkg) {
				return entry{provider: provider, spec: pkg}, true
			}
		}
	}
	return entry{}, false
}

// packageName is the validator for a provider's package spec: an npm name may
// carry an @scope/ prefix, a pypi or crate name may not.
func packageName(provider string) *regexp.Regexp {
	if provider == constant.ProviderNpm {
		return npmPackage
	}
	return segment
}

// repository returns the owner/repo of the tool's first GitHub-shaped backend
// in list order, ok=false when no backend is GitHub-shaped with a clean
// two-segment spec (a monorepo sub-path pins versions under tags the bare
// repository lookup would misread, so it is skipped).
func repository(backends []any) (string, bool) {
	for _, backend := range backends {
		for _, scheme := range githubBackends {
			spec, ok := strings.CutPrefix(backendSpec(backend), scheme)
			if !ok {
				continue
			}
			spec, _, _ = strings.Cut(spec, "[") // drop a [option] qualifier
			own, repo, ok := strings.Cut(spec, "/")
			if ok && owner.MatchString(own) && segment.MatchString(repo) {
				return own + "/" + repo, true
			}
		}
	}
	return "", false
}

// providerMap describes one emitted map: its Go variable, the provider whose
// tools populate it, and the sentence completing the map's doc comment.
type providerMap struct {
	name     string
	provider string
	values   string
}

// providerMaps are the generated maps in file order, github first so the
// existing lookup path reads an unchanged declaration.
var providerMaps = []providerMap{
	{
		"miseRegistryTools",
		constant.ProviderGithub,
		"the GitHub repository their default backend tracks",
	},
	{"misePypiTools", constant.ProviderPypi, "the PyPI package their pipx backend installs"},
	{"miseNpmTools", constant.ProviderNpm, "the npm package their npm backend installs"},
	{"miseCratesTools", constant.ProviderCrates, "the crate their cargo backend installs"},
}

// render emits the generated Go source for the per-provider tool maps,
// gofmt-formatted.
func render(tools map[string]entry, ref string) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(
		&b,
		"// Code generated by ./internal/tools/genmise from the mise registry (%s); DO NOT EDIT.\n//\n// Regenerate with: go generate ./internal/match\n\npackage match\n",
		ref,
	)
	for _, m := range providerMaps {
		writeMap(&b, m, tools)
	}
	return format.Source(b.Bytes())
}

// writeMap appends one provider's map literal to b, its doc comment included,
// with the tool names naturally sorted.
func writeMap(b *bytes.Buffer, m providerMap, tools map[string]entry) {
	fmt.Fprintf(
		b,
		"\n// %s maps mise registry tool names (and aliases) to %s.\nvar %s = map[string]string{\n",
		m.name,
		m.values,
		m.name,
	)
	specs := map[string]string{}
	for name, e := range tools {
		if e.provider == m.provider {
			specs[name] = e.spec
		}
	}
	for _, name := range xmaps.KeysNatural(specs) {
		fmt.Fprintf(b, "\t%q: %q,\n", name, specs[name])
	}
	b.WriteString("}\n")
}
