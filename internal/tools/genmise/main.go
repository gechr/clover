// Command genmise generates the mise tool-name to GitHub repository map the
// match package's auto-detection uses for mise configuration files. It reads
// the registry directory of a mise checkout (github.com/jdx/mise), keeps every
// tool whose default (first) backend names a GitHub repository - an aqua:,
// github:, or ubi: backend - and emits the sorted map to the output file.
//
// Tools whose default backend is another ecosystem (npm:, pipx:, cargo:,
// core:, ...) are skipped rather than mapped through a fallback backend, since
// the versions a user pins for them follow that ecosystem's scheme, not the
// GitHub tags. Tools the match package already maps by hand (HashiCorp
// products, the Go toolchain, the Node.js runtime) are skipped too, so the
// curated mappings stay authoritative.
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
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
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

// curated are the tool names the match package maps by hand; the generator
// never emits them so the curated entries stay authoritative.
var curated = []string{
	// HashiCorp products, mapped to the hashicorp provider.
	"boundary", "consul", "nomad", "packer", "terraform", "vagrant", "vault", "waypoint",
	// Mapped by hand in the match package.
	"bun", "deno", "elixir", "erlang", "go", "node", "opentofu", "python", "rust", "tofu", "zig",
}

// owner validates the owner segment of an owner/repo spec. A GitHub owner is
// alphanumerics and hyphens only, so an aqua HTTP-type package whose first
// segment is a domain (atlassian.com/acli) is rejected.
var owner = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// segment validates the repository segment (and an alias name), where dots and
// underscores are legal.
var segment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

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
	log.Printf("wrote %d tools to %s (mise %s)", len(tools), out, ref)
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
	log.Printf("cloning %s at %s", miseRepo, ref)
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
// alias) to repository map.
func read(dir string) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no registry files under %q", dir)
	}

	tools := map[string]string{}
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
		repo, ok := repository(tool.Backends)
		if !ok {
			continue
		}
		tools[name] = repo
		for _, alias := range tool.Aliases {
			if segment.MatchString(alias) && !slices.Contains(curated, alias) {
				tools[alias] = repo
			}
		}
	}
	return tools, nil
}

// repository returns the owner/repo of the tool's default backend, ok=false
// when that backend is not GitHub-shaped or its spec is not a clean two-segment
// path (a monorepo sub-path pins versions under tags the bare repository
// lookup would misread, so it is skipped).
func repository(backends []any) (string, bool) {
	if len(backends) == 0 {
		return "", false
	}
	spec := ""
	for _, scheme := range githubBackends {
		if rest, ok := strings.CutPrefix(backendSpec(backends[0]), scheme); ok {
			spec = rest
			break
		}
	}
	if spec == "" {
		return "", false
	}
	spec, _, _ = strings.Cut(spec, "[") // drop a [option] qualifier
	own, repo, ok := strings.Cut(spec, "/")
	if !ok || !owner.MatchString(own) || !segment.MatchString(repo) {
		return "", false
	}
	return own + "/" + repo, true
}

// render emits the generated Go source for the tool map, gofmt-formatted.
func render(tools map[string]string, ref string) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(
		&b,
		`// Code generated by internal/tools/genmise from the mise registry (%s); DO NOT EDIT.

package match

// miseRegistryTools maps mise registry tool names (and aliases) to the GitHub
// repository their default backend tracks. Regenerate with:
//
//	go generate ./internal/match
var miseRegistryTools = map[string]string{
`,
		ref,
	)
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		fmt.Fprintf(&b, "\t%q: %q,\n", name, tools[name])
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}
