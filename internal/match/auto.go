package match

import (
	"regexp"
	"strings"

	"github.com/gechr/clover/internal/constant"
	xmaps "github.com/gechr/x/maps"
	xslices "github.com/gechr/x/slices"
)

// Inference is what auto-detection resolved for a `provider=auto` marker from
// its target line: the real provider plus any provider parameters readable from
// the line. Empty parameter fields mean the line did not carry that detail.
// A follower is the one shape whose inference names no upstream of its own: it
// carries From, Value and Pattern instead, and its Provider is
// [constant.ProviderFollow] so the walk below does not read it as a decline.
// ID is the mirror of From, set on the producer a follower was paired with.
type Inference struct {
	Chart      string
	From       string
	Host       string
	ID         string
	Package    string
	Pattern    string
	Product    string
	Provider   string
	Registry   string
	Repository string
	Source     string
	TagPrefix  string
	Track      string
	Value      string
}

// Follower reports whether the inference projects another marker's value onto
// its line rather than resolving an upstream itself, so a caller renders From,
// Value and Pattern in place of a provider.
func (i Inference) Follower() bool { return i.Provider == constant.ProviderFollow }

// Missing reports why the inference cannot resolve - a route matched but the
// line carries no usable reference - or "" when the inference is complete. The
// forge and image providers need a repository, hashicorp needs a product, and
// node needs nothing beyond the provider itself.
func (i Inference) Missing() string {
	switch i.Provider {
	case constant.ProviderFollow:
		// A follower reads no upstream, so what it needs is the producer it
		// projects, the value it projects, and - for a sha256, which is fetched
		// rather than projected - the asset that sum belongs to.
		if i.From == "" {
			return "line names no producer to follow"
		}
		if i.Value == "" {
			return "line names no value to project"
		}
		if i.Value == constant.ValueSha256 && i.Pattern == "" {
			return "no asset filename to source the sum from"
		}
	case constant.ProviderDocker,
		constant.ProviderGitea,
		constant.ProviderGithub,
		constant.ProviderGitlab:
		if i.Repository == "" {
			return "reference has no repository"
		}
	case constant.ProviderHashicorp:
		if i.Product == "" {
			return "line names no product"
		}
	case constant.ProviderPypi, constant.ProviderNpm, constant.ProviderCrates:
		if i.Package == "" {
			return "line names no package"
		}
	case constant.ProviderTerraform, constant.ProviderOpentofu:
		if i.Source == "" {
			return "block names no source"
		}
	case constant.ProviderHelm:
		if i.Chart == "" || i.Registry == "" {
			return "dependency names no chart or repository"
		}
	}
	return ""
}

// subject is what a route's inference reads: the file's lines and the index of
// the target line within them. Most shapes carry their reference on the target
// line alone, but some name it on a sibling (a Helm dependency's chart, a
// Terraform entry's source), so the whole file is in reach.
//
// It deliberately does not carry the file's path. The route that matched was
// already selected by its path guard, so an inference that consulted the path
// would be re-deciding which shape it is looking at - the guess-chain this
// dispatch exists to abolish.
type subject struct {
	lines  []string
	target int
}

// line returns the target line. A subject is only ever built for an in-range
// target, so the index is always valid.
func (s subject) line() string { return s.lines[s.target] }

// inferFunc reads the provider parameters the shape a route matched carries.
// Each route owns its own, so the shape that matched is the shape that is read:
// no inference re-derives which route it came from by re-testing path globs.
type inferFunc func(s subject) Inference

// Table is the dispatch table scoped to one file: the routes whose path guard
// matches the file, computed once so per-line inference never re-evaluates a
// path glob. Inference is called for every line of every scanned file, and the
// glob matching dwarfs the line matching, so the scoping is what makes a large
// tree affordable.
type Table struct {
	path   string
	routes []route
}

// NewTable scopes the dispatch table to the file at path.
func NewTable(path string) Table {
	scoped := make([]route, 0, len(routes))
	for _, r := range routes {
		if r.when.path == "" || matchPath(r.when.path, path) {
			scoped = append(scoped, r)
		}
	}
	return Table{path: path, routes: scoped}
}

// Infer resolves the provider for an `auto` marker from its target line
// (lines[target]), reusing the dispatch routes: the first route whose line
// matches names the provider through its own inference, which also reads the
// provider's parameters - the repository from a GitHub Actions pin, the
// registry and repository from a container image reference - so a bare
// `provider=auto` needs no further keys. A route with no inference (the smart
// catch-all, a follower) is skipped, since it names no provider to infer. It
// returns ok=false when nothing matches, leaving the marker for the caller to
// reject.
//
// An inference may decline the line by naming no provider, and the walk
// continues past it. A shape whose provider is not fixed by the route reads it
// from the file - a pre-commit rev is tracked on whichever forge its repo: URL
// names - so recognizing the shape and resolving it to a provider are two steps,
// and only the second can fail. Declining keeps that failure indistinguishable
// from never having matched, rather than surfacing a provider-less inference
// that every caller downstream would have to guard against.
func (t Table) Infer(lines []string, target int) (Inference, bool) {
	if target < 0 || target >= len(lines) {
		return Inference{}, false
	}
	s := subject{lines: lines, target: target}
	line := s.line()
	for _, r := range t.routes {
		if r.infer == nil {
			continue
		}
		if r.when.lineMatch != nil && !r.when.lineMatch.Matches(line) {
			continue
		}
		if inferred := r.infer(s); inferred.Provider != "" {
			return inferred, true
		}
	}
	return Inference{}, false
}

// Infer is the one-shot form of [Table.Infer], for a caller inferring a single
// line. A loop over a file's lines builds one [Table] instead.
func Infer(path string, lines []string, target int) (Inference, bool) {
	return NewTable(path).Infer(lines, target)
}

// gitlabHost is the host the gitlab provider targets when host is omitted. A
// component reference on it infers no host key, so the directive stays minimal.
const gitlabHost = "gitlab.com"

// componentReference extracts the host and project path from a GitLab CI/CD
// component reference, e.g. "component: gitlab.com/org/proj/comp@1.2.3" ->
// ("", "org/proj"). The component name (the last path segment) is dropped,
// since versions are the project's tags, and the default gitlab.com host is
// returned empty. A first segment that does not look like a host, or a
// reference carrying a variable like $CI_SERVER_FQDN, yields no reference.
func componentReference(line string) (string, string) {
	_, after, ok := strings.Cut(line, "component:")
	if !ok {
		return "", ""
	}
	ref := yamlScalar(after)
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at]
	}
	segments := strings.Split(ref, "/")
	if len(segments) < 3 || !strings.Contains(segments[0], ".") ||
		strings.ContainsRune(ref, '$') {
		return "", ""
	}
	host := segments[0]
	if host == gitlabHost {
		host = ""
	}
	return host, strings.Join(segments[1:len(segments)-1], "/")
}

// terraformProduct is the releases.hashicorp.com slug a Terraform
// required_version constraint tracks.
const terraformProduct = "terraform"

// keyFunc extracts a tool name from a line in one of the two formats mise
// reads. The two differ only in how the key is spelled, so a route pairs its
// path guard with the matching reader rather than having the reader re-test the
// path it was already routed by.
type keyFunc func(line string) string

// miseKey extracts the tool name from a mise configuration line, the quoted or
// bare TOML key before =, e.g. `terraform = "1.9.8"` -> "terraform".
func miseKey(line string) string {
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.Trim(strings.TrimSpace(key), `"'`)
}

// toolVersionsKey extracts the tool name from a .tool-versions line, the first
// whitespace-separated field, e.g. `terraform 1.9.8` -> "terraform".
func toolVersionsKey(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// pypiPackage extracts the package name from the first dependency specifier
// on the line, as the project spells it - the provider normalizes it per
// PEP 503. The requirement rewriter demands exactly one specifier, so when the
// marker resolves, the first is the only one and the name always belongs to
// the version bumped.
func pypiPackage(line string) string {
	specs := requirementSpecs(line)
	if len(specs) == 0 {
		return ""
	}
	return specs[0].name
}

// githubTool is the GitHub source a well-known tool name maps to: the
// repository whose tags carry its releases, and the tag-prefix those tags wear
// when they are not bare versions (golang/go tags releases goX.Y.Z).
type githubTool struct {
	repository string
	tagPrefix  string
}

// tofuTool is the source of OpenTofu toolchain releases, referenced by both
// the mise tofu tool and a .tofu file's required_version constraint.
var tofuTool = githubTool{repository: "opentofu/opentofu"}

// miseGithubTools maps curated mise tool names to the GitHub source whose
// releases they track, taking precedence over the generated registry map for
// tools whose tags carry a prefix or that the registry routes elsewhere. The
// long tail of well-known tools lives in miseRegistryTools, generated from the
// mise registry:
//
//go:generate go run ../tools/genmise
var miseGithubTools = map[string]githubTool{
	"opentofu": tofuTool,
	"tofu":     tofuTool,
	// Core mise runtimes (backends = ["core:..."]) whose upstream tags cleanly
	// carry the pinned version. Runtimes with exotic tag schemes (ruby's v3_4_1,
	// swift's swift-6.0-RELEASE, multi-vendor java) are deliberately absent.
	"bun":    {repository: "oven-sh/bun", tagPrefix: "bun-"},
	"deno":   {repository: "denoland/deno"},
	"elixir": {repository: "elixir-lang/elixir"},
	"erlang": {repository: "erlang/otp", tagPrefix: "OTP-"},
	// rust has a native provider, which the dedicated mise route infers first;
	// the mapping remains so an explicit provider=github tool=rust keeps
	// resolving against the repository's tags.
	"rust": {repository: "rust-lang/rust"},
}

// provides returns an inference naming only the provider, for a shape that
// carries no parameters of its own - a toolchain pin, whose provider needs
// nothing beyond the version already on the line.
func provides(name string) inferFunc {
	return func(subject) Inference { return Inference{Provider: name} }
}

// inferImage reads a container image reference: its registry and repository,
// plus the floating tag a digest pin should keep fresh.
func inferImage(s subject) Inference {
	line := s.line()
	registry, repository := imageReference(line)
	return Inference{
		Provider:   constant.ProviderDocker,
		Registry:   registry,
		Repository: repository,
		Track:      trackedTag(imageToken(line)),
	}
}

// inferAction reads the repository a GitHub Actions uses: reference names.
func inferAction(s subject) Inference {
	return Inference{
		Provider:   constant.ProviderGithub,
		Repository: actionRepository(s.line()),
	}
}

// inferComponent reads the host and project a GitLab CI/CD component include
// names.
func inferComponent(s subject) Inference {
	host, repository := componentReference(s.line())
	return Inference{Provider: constant.ProviderGitlab, Host: host, Repository: repository}
}

// inferTofuToolchain names the OpenTofu source a .tofu file's required_version
// constraint tracks. The repository is constant: the file's extension is the
// whole signal, so there is nothing on the line to read.
func inferTofuToolchain(subject) Inference {
	return Inference{
		Provider:   constant.ProviderGithub,
		Repository: tofuTool.repository,
		TagPrefix:  tofuTool.tagPrefix,
	}
}

// inferTerraformToolchain names the product a Terraform required_version
// constraint pins, which is always terraform itself.
func inferTerraformToolchain(subject) Inference {
	return Inference{Provider: constant.ProviderHashicorp, Product: terraformProduct}
}

// inferMiseGithub reads the GitHub repository a mise tool key tracks, and the
// tag prefix its upstream tags wear.
func inferMiseGithub(key keyFunc) inferFunc {
	return func(s subject) Inference {
		repository, tagPrefix := miseTool(key(s.line()))
		return Inference{
			Provider:   constant.ProviderGithub,
			Repository: repository,
			TagPrefix:  tagPrefix,
		}
	}
}

// inferMiseHashicorp reads the HashiCorp product a mise tool key names; the
// tool name doubles as the product slug on releases.hashicorp.com.
func inferMiseHashicorp(key keyFunc) inferFunc {
	return func(s subject) Inference {
		return Inference{Provider: constant.ProviderHashicorp, Product: key(s.line())}
	}
}

// inferMisePackage reads the ecosystem package a mise tool key installs, for
// the pypi, npm, and crates routes whose tools name one in the generated maps.
func inferMisePackage(name string, key keyFunc) inferFunc {
	return func(s subject) Inference {
		return Inference{Provider: name, Package: misePackage(key(s.line()))}
	}
}

// inferRequirement reads the package a PEP 508 dependency specifier names.
func inferRequirement(s subject) Inference {
	return Inference{Provider: constant.ProviderPypi, Package: pypiPackage(s.line())}
}

// inferRegistrySource reads the source address governing the target line's
// version constraint, which lives on a sibling line of the enclosing block. Both
// things a Terraform registry serves are reached the same way: a provider named
// by a required_providers entry, or a module named by a module block. A version
// belonging to neither infers no source, so the marker is left unresolved rather
// than pointed at a registry that never served it.
func inferRegistrySource(name string) inferFunc {
	return func(s subject) Inference {
		return Inference{Provider: name, Source: registrySource(s.lines, s.target)}
	}
}

// inferHelmDependency reads the chart and repository of the Chart.yaml
// dependencies entry the target line's version scalar belongs to, both of which
// live on sibling lines of the entry.
func inferHelmDependency(s subject) Inference {
	chart, registry := helmDependency(s.lines, s.target)
	return Inference{Provider: constant.ProviderHelm, Chart: chart, Registry: registry}
}

// LookupTool resolves a mise tool name to the GitHub repository whose tags
// carry its releases and the tag prefix those tags wear, consulting the
// curated [miseGithubTools] map then the generated registry map.
func LookupTool(name string) (string, string, bool) {
	if tool, found := miseGithubTools[name]; found {
		return tool.repository, tool.tagPrefix, true
	}
	if repo, found := miseRegistryTools[name]; found {
		return repo, "", true
	}
	return "", "", false
}

// ToolNames returns every tool name [LookupTool] resolves, naturally sorted,
// so a mistyped name can be met with a suggestion. It covers the GitHub-released
// tools only, the ones a `tool=` key names; an ecosystem tool is reached by its
// package name, not this map.
func ToolNames() []string {
	names := xslices.Union(
		xmaps.Keys(miseGithubTools),
		xmaps.Keys(miseRegistryTools),
	)
	xslices.SortNatural(names)
	return names
}

// pypiToolNames, npmToolNames, and cratesToolNames return the mise tool names
// whose only backend is a pipx:, npm:, or cargo: package - the keys of the
// generated ecosystem maps, naturally sorted so each route's alternation is a
// stable string. The four name sets (these three and [ToolNames]) are disjoint,
// since a tool resolves to exactly one provider.
func pypiToolNames() []string   { return sortedKeys(misePypiTools) }
func npmToolNames() []string    { return sortedKeys(miseNpmTools) }
func cratesToolNames() []string { return sortedKeys(miseCratesTools) }

// sortedKeys returns m's keys in natural order.
func sortedKeys(m map[string]string) []string {
	names := xmaps.Keys(m)
	xslices.SortNatural(names)
	return names
}

// miseEcosystems pairs each generated ecosystem map with the provider whose
// registry serves it. Both readers below walk this one list, so a fourth
// ecosystem cannot be added to one and missed by the other.
var miseEcosystems = []struct {
	tools    map[string]string
	provider string
}{
	{misePypiTools, constant.ProviderPypi},
	{miseNpmTools, constant.ProviderNpm},
	{miseCratesTools, constant.ProviderCrates},
}

// misePackage returns the ecosystem package a mise tool key installs - the
// pipx:, npm:, or cargo: backend the generated maps recorded - or "" when the
// key names no ecosystem tool. A tool resolves to one provider, so it appears in
// at most one map.
func misePackage(key string) string {
	pkg, _ := miseEcosystem(key)
	return pkg
}

// miseEcosystem returns both halves of that answer: the package and the provider
// that resolves it. A route knows its own provider and needs only the package;
// an inference reading a tool name from a variable has neither.
func miseEcosystem(key string) (string, string) {
	for _, e := range miseEcosystems {
		if pkg, ok := e.tools[key]; ok {
			return pkg, e.provider
		}
	}
	return "", ""
}

// miseTool extracts the GitHub source a mise tool key tracks: a curated tool
// name from [miseGithubTools], a registry tool name from the generated
// miseRegistryTools, or a github: or ubi: backend key, e.g.
// `"ubi:owner/tool" = "1.2.3"` -> "owner/tool", dropping a trailing [option]
// qualifier. It returns empty strings when the key names no repository.
func miseTool(key string) (string, string) {
	if repo, prefix, ok := LookupTool(key); ok {
		return repo, prefix
	}
	for _, scheme := range []string{"github:", "ubi:"} {
		repo, ok := strings.CutPrefix(key, scheme)
		if !ok {
			continue
		}
		repo, _, _ = strings.Cut(repo, "[")
		if strings.Count(repo, "/") != 1 {
			return "", "" // a backend repository is exactly owner/repo
		}
		return repo, ""
	}
	return "", ""
}

// actionRepository extracts the owner/repo from a GitHub Actions uses: pin,
// e.g. "uses: gechr/actions/.github/workflows/lint.yaml@<sha>" -> "gechr/actions".
// It returns "" when the line is not an owner/repo reference.
func actionRepository(line string) string {
	_, after, ok := strings.Cut(line, "uses:")
	if !ok {
		return ""
	}
	ref := yamlScalar(after) // a quoted "owner/repo@sha" or a trailing # comment
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at]
	}
	owner, rest, ok := strings.Cut(ref, "/")
	if !ok || owner == "" {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	if name == "" {
		return ""
	}
	return owner + "/" + name
}

// imageReference splits the registry host and repository path from a container
// image reference on a FROM or image: line, e.g.
// "FROM ghcr.io/owner/img:1.2" -> ("ghcr.io", "owner/img") and
// "FROM nginx:1.27" -> ("", "nginx"). The registry is empty for Docker Hub,
// where the first segment is a path component, not a host.
func imageReference(line string) (string, string) {
	ref := imageToken(line)
	if ref == "" {
		return "", ""
	}
	if at := strings.IndexByte(ref, '@'); at >= 0 {
		ref = ref[:at] // drop a digest pin
	}

	registry := ""
	remainder := ref
	if slash := strings.IndexByte(ref, '/'); slash >= 0 && isRegistryHost(ref[:slash]) {
		registry = ref[:slash]
		remainder = ref[slash+1:]
	}
	if colon := strings.LastIndexByte(remainder, ':'); colon >= 0 {
		remainder = remainder[:colon] // drop the tag (the host's port already split off)
	}
	return registry, remainder
}

// trackedTag returns the floating tag of a digest-pinned image reference
// (nonroot, latest, stable), or "" when the reference is not digest-pinned,
// carries no tag, or its tag is not a floating name. A digest pin on a floating
// tag can only mean one thing: keep the digest fresh for that tag, which is
// exactly what track does.
func trackedTag(ref string) string {
	before, _, pinned := strings.Cut(ref, "@")
	if !pinned {
		return ""
	}
	start, ok := imageTagStart(before)
	if !ok {
		return ""
	}
	tag := before[start:]
	if !floatingTag.MatchString(tag) {
		return ""
	}
	return tag
}

// floatingTag matches a floating tag name: lowercase letters and interior
// hyphens only (root, nonroot, latest, stable-slim). A tag carrying digits is
// either a version for selection to bump or too ambiguous to assume tracking
// for, so it never infers track.
var floatingTag = regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)

// dockerScheme prefixes the image reference of a workflow container job:
// uses: docker://<image>.
const dockerScheme = "docker://"

// imageToken extracts the image reference from a Dockerfile FROM instruction, a
// workflow container job's uses: docker:// reference, or a YAML image: mapping,
// returning "" when the line carries none. The uses: branch runs before the
// image: cut, which would otherwise split inside a reference like myimage:1.2.
func imageToken(line string) string {
	line = strings.TrimSpace(line)
	if rest, ok := strings.CutPrefix(line, "FROM "); ok {
		return fromImage(rest)
	}
	if _, after, ok := strings.Cut(line, "uses:"); ok {
		img, ok := strings.CutPrefix(yamlScalar(after), dockerScheme)
		if !ok {
			return "" // a uses: without the docker:// scheme is an action, not an image
		}
		return img
	}
	if _, after, ok := strings.Cut(line, "image:"); ok {
		return yamlScalar(after)
	}
	return ""
}

// fromImage returns the image from the arguments of a FROM instruction, skipping
// flags like --platform= and ignoring a trailing AS stage name.
func fromImage(rest string) string {
	for field := range strings.FieldsSeq(rest) {
		if strings.HasPrefix(field, "--") {
			continue
		}
		return field // the first non-flag token is the image
	}
	return ""
}

// isRegistryHost reports whether a reference's first segment is a registry host
// rather than a repository path component: a host carries a dot or port, or is
// the special localhost.
func isRegistryHost(segment string) bool {
	return segment == "localhost" || strings.ContainsAny(segment, ".:")
}

// yamlScalar extracts the value of a YAML mapping scalar - an image: or uses:
// value - stripping surrounding quotes and any inline comment. A quoted scalar
// ends at its closing quote, so a trailing `# comment` is dropped (`"nginx:1.27"
// # pin` -> `nginx:1.27`); an unquoted scalar ends at an inline ` #` comment.
// Without this the quote or comment rides along into the reference and the
// repository is misread (`"actions/checkout` instead of `actions/checkout`).
//
// This stays a line-level reader, not a YAML parser: it honors each quote
// style's escape rule so the closing quote is found correctly, but does not
// interpret the richer escapes (\n, \uXXXX, block scalars) that never appear in a
// version reference. An exotic value it cannot read becomes a reference the
// verify gate rejects, so the line is skipped rather than misread.
func yamlScalar(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, `"`):
		return doubleQuoted(s[1:])
	case strings.HasPrefix(s, `'`):
		return singleQuoted(s[1:])
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i] // an inline comment on an unquoted scalar
	}
	return strings.TrimSpace(s)
}

// doubleQuoted returns the value of a YAML double-quoted scalar from the text
// after its opening quote: it ends at the first unescaped ", unescaping \" and \\
// (the only escapes a version reference can carry). An unterminated quote yields
// the rest of the line.
func doubleQuoted(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		if s[i] == '"' {
			break // the closing quote
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// singleQuoted returns the value of a YAML single-quoted scalar from the text
// after its opening quote: it ends at the first single quote that is not doubled,
// since YAML escapes a literal quote by doubling it. An unterminated quote yields
// the rest of the line.
func singleQuoted(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			break // the closing quote
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
