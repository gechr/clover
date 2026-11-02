// Command genmise generates the maps the match package's auto-detection uses to
// resolve a mise tool name to the upstream whose versions it tracks. It reads
// the registry directory of a mise checkout (github.com/jdx/mise) and records,
// for each tool, the provider its backends prove carry the pinned versions.
//
// A tool's backends are interchangeable installers for the same pinned version.
// A GitHub-shaped backend - aqua:, github:, or ubi:, in any position - is
// preferred, since GitHub tags are the most universal version source, and the
// tool maps to that owner/repo under the github provider, along with the
// backend's version_prefix when its tags carry one. A tool with no GitHub
// backend falls back to its first package-manager backend: pipx: to pypi, npm:
// to npm, cargo: to crates, mapping to the package that ecosystem installs.
// Tools backed only by an unsupported ecosystem (core:, asdf:, gem:, ...) are
// skipped, as are tools the match package already maps by hand (HashiCorp
// products, the Go toolchain, the Node.js runtime), so the curated mappings
// stay authoritative.
//
// The output declares one map per provider: miseRegistryTools (github, name to
// owner/repo), misePypiTools, miseNpmTools, and miseCratesTools (name to
// package), plus miseRegistryTagPrefixes (github, name to tag prefix) for the
// GitHub tools whose tags carry one.
//
// Usage:
//
//	go run ./internal/tools/genmise [-src <mise-checkout>] [-local <dir>] [-ref <branch>] [-o <file>]
//
// Without -src the mise repository is shallow-cloned to a temporary directory
// at the -ref branch (main by default), and the header records the commit the
// map was generated from.
//
// With -local, the TOMLs in that directory - the same format as mise's own
// registry - are merged in for tools mise does not carry. Such an entry exists
// only to fill that gap, so generation fails once mise declares the name or one
// of its aliases: the failure says the entry is no longer ours to maintain and
// should be deleted, rather than letting it silently shadow mise's.
package main

import (
	"bytes"
	"context"
	"errors"
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

// errNoRegistry reports a registry directory holding no tool files. It is fatal
// for the mise checkout but not for the local registry, which empties once mise
// has caught up with every tool it carried.
var errNoRegistry = errors.New("no registry files")

// registryTool is the subset of a mise registry entry the generator reads.
// Backends decodes loosely: an entry is usually a plain "scheme:spec" string,
// but may be a table ({ full = "...", options = ... }) whose full key carries
// the same spec.
type registryTool struct {
	Backends []any    `toml:"backends"`
	Aliases  []string `toml:"aliases"`
}

// entry is a resolved registry tool: the Clover provider whose ecosystem
// carries the tool's versions, the provider-specific spec the map records - a
// GitHub owner/repo, or a package name - and, for a GitHub tool, the prefix its
// release tags carry ahead of the version.
type entry struct {
	provider  string
	spec      string
	tagPrefix string
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

// backendOption returns the named option of a backend entry, "" when it sets
// none. A table entry carries options in its options table; a string entry
// carries them as a trailing [key=value,...] qualifier on its spec.
func backendOption(backend any, key string) string {
	if b, ok := backend.(map[string]any); ok {
		options, _ := b["options"].(map[string]any)
		value, _ := options[key].(string)
		return value
	}
	spec, _ := backend.(string)
	_, qualifier, ok := strings.Cut(spec, "[")
	if !ok {
		return ""
	}
	qualifier, _, _ = strings.Cut(qualifier, "]")
	for option := range strings.SplitSeq(qualifier, ",") {
		if k, v, ok := strings.Cut(option, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
	local := flag.String("local", "", "directory of registry TOMLs for tools mise does not carry")
	ref := flag.String("ref", "main", "mise ref to generate from, recorded in the header")
	out := flag.String("o", "zz_generated.miseregistry.go", "output file")
	flag.Parse()
	log.SetFlags(0)

	if err := run(context.Background(), *src, *local, *ref, *out); err != nil {
		log.Fatal(err)
	}
}

// run generates the map from the checkout at src (cloning one when empty),
// merged with the local registry when local is set, and writes it to out. It is
// main's body, split out so its defers run before the fatal-exit error handling.
func run(ctx context.Context, src, local, ref, out string) error {
	if src == "" {
		clone, cleanup, err := cloneMise(ctx, ref)
		if err != nil {
			return err
		}
		defer cleanup()
		src = clone
	}

	tools, declared, err := read(filepath.Join(src, "registry"))
	if err != nil {
		return err
	}
	if local != "" {
		localTools, localDeclared, readErr := read(local)
		if readErr != nil && !errors.Is(readErr, errNoRegistry) {
			return readErr
		}
		if err = merge(tools, declared, localTools, localDeclared); err != nil {
			return err
		}
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
// alias) to resolved-entry map, along with every name and alias the files
// declare - resolvable or not - mapped to the file declaring it.
func read(dir string) (map[string]entry, map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("%w under %q", errNoRegistry, dir)
	}

	tools := map[string]entry{}
	declared := map[string]string{}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".toml")
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, err
		}
		var tool registryTool
		if err := toml.Unmarshal(data, &tool); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", file, err)
		}
		declared[name] = file
		for _, alias := range tool.Aliases {
			declared[alias] = file
		}
		if slices.Contains(curated, name) {
			continue
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
	return tools, declared, nil
}

// merge folds the local registry's tools into the mise ones. A local entry
// exists only because mise lacks the tool, so one whose name or alias mise now
// declares fails, telling the maintainer to delete it; so does one that resolves
// to nothing, being curated or backed only by an unsupported ecosystem.
func merge(tools map[string]entry, declared map[string]string,
	localTools map[string]entry, localDeclared map[string]string,
) error {
	for _, name := range xmaps.KeysNatural(localDeclared) {
		file := localDeclared[name]
		if miseFile, ok := declared[name]; ok {
			return fmt.Errorf(
				"%s: mise now declares %q (%s); delete the local entry",
				file, name, filepath.Base(miseFile),
			)
		}
		resolved, ok := localTools[name]
		if !ok {
			return fmt.Errorf("%s: %q is curated or has no supported backend", file, name)
		}
		tools[name] = resolved
	}
	return nil
}

// resolve returns the provider and spec the tool's backends prove carry its
// versions: a GitHub owner/repo and tag prefix from the first github-shaped
// backend (preferred regardless of list position, since GitHub tags are the most
// universal version source), else the package name of the first pipx:/npm:/cargo:
// backend. ok is false when no backend maps to a supported provider.
func resolve(backends []any) (entry, bool) {
	if repo, prefix, ok := repository(backends); ok {
		return entry{provider: constant.ProviderGithub, spec: repo, tagPrefix: prefix}, true
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
// in list order and the version_prefix its tags carry (azure-cli-2.67.0 for
// azure), ok=false when no backend is GitHub-shaped with a clean two-segment
// spec (a monorepo sub-path pins versions under tags the bare repository lookup
// would misread, so it is skipped).
func repository(backends []any) (string, string, bool) {
	for _, backend := range backends {
		for _, scheme := range githubBackends {
			spec, ok := strings.CutPrefix(backendSpec(backend), scheme)
			if !ok {
				continue
			}
			spec, _, _ = strings.Cut(spec, "[") // drop a [option] qualifier
			own, repo, ok := strings.Cut(spec, "/")
			if ok && owner.MatchString(own) && segment.MatchString(repo) {
				return own + "/" + repo, backendOption(backend, "version_prefix"), true
			}
		}
	}
	return "", "", false
}

// providerMap describes one emitted map: its Go variable, the provider whose
// tools populate it, the sentence completing the map's doc comment, and the
// entry field it records. A tool whose field is empty is left out.
type providerMap struct {
	name     string
	provider string
	values   string
	value    func(entry) string
}

// spec and tagPrefix read the entry field a providerMap records.
func spec(e entry) string      { return e.spec }
func tagPrefix(e entry) string { return e.tagPrefix }

// providerMaps are the generated maps in file order, github first so the
// existing lookup path reads an unchanged declaration.
var providerMaps = []providerMap{
	{
		"miseRegistryTools",
		constant.ProviderGithub,
		"the GitHub repository their default backend tracks",
		spec,
	},
	{
		"miseRegistryTagPrefixes",
		constant.ProviderGithub,
		"the prefix their GitHub release tags carry ahead of the version",
		tagPrefix,
	},
	{"misePypiTools", constant.ProviderPypi, "the PyPI package their pipx backend installs", spec},
	{"miseNpmTools", constant.ProviderNpm, "the npm package their npm backend installs", spec},
	{"miseCratesTools", constant.ProviderCrates, "the crate their cargo backend installs", spec},
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
		if value := m.value(e); e.provider == m.provider && value != "" {
			specs[name] = value
		}
	}
	for _, name := range xmaps.KeysNatural(specs) {
		fmt.Fprintf(b, "\t%q: %q,\n", name, specs[name])
	}
	b.WriteString("}\n")
}
