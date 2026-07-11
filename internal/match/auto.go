package match

import (
	"regexp"
	"strings"

	"github.com/gechr/clover/internal/constant"
)

// Inference is what auto-detection resolved for a `provider=auto` marker from
// its target line: the real provider plus any provider parameters readable from
// the line. Empty parameter fields mean the line did not carry that detail.
type Inference struct {
	Host       string
	Product    string
	Provider   string
	Registry   string
	Repository string
	Source     string
	TagPrefix  string
	Track      string
}

// Missing reports why the inference cannot resolve - a route matched but the
// line carries no usable reference - or "" when the inference is complete. The
// forge and image providers need a repository, hashicorp needs a product, and
// node needs nothing beyond the provider itself.
func (i Inference) Missing() string {
	switch i.Provider {
	case constant.ProviderDocker,
		constant.ProviderGithub,
		constant.ProviderGitlab:
		if i.Repository == "" {
			return "reference has no repository"
		}
	case constant.ProviderHashicorp:
		if i.Product == "" {
			return "line names no product"
		}
	case constant.ProviderTerraform, constant.ProviderOpentofu:
		if i.Source == "" {
			return "block names no source"
		}
	}
	return ""
}

// Infer resolves the provider for an `auto` marker from its file path and
// target line (lines[target]), reusing the dispatch routes: the first route
// whose path and line match (ignoring its provider guard, which is the answer)
// names the provider. It also reads the provider's parameters from the line -
// the repository from a GitHub Actions pin, the registry and repository from a
// container image reference - so a bare `provider=auto` needs no further keys.
// The surrounding lines matter only to the Terraform route, whose source
// address lives on a sibling line of its block. It returns ok=false when
// nothing matches, leaving the marker for the caller to reject.
func Infer(path string, lines []string, target int) (Inference, bool) {
	if target < 0 || target >= len(lines) {
		return Inference{}, false
	}
	line := lines[target]
	for _, r := range routes {
		c := r.when
		if c.provider == "" {
			continue // the smart catch-all infers nothing
		}
		if c.path != "" && !matchPath(c.path, path) {
			continue
		}
		if c.lineMatch != nil && !c.lineMatch.Matches(line) {
			continue
		}
		inferred := Inference{Provider: c.provider}
		switch c.provider {
		case constant.ProviderGithub:
			inferred.Repository, inferred.TagPrefix = githubReference(path, line)
		case constant.ProviderDocker:
			inferred.Registry, inferred.Repository = imageReference(line)
			inferred.Track = trackedTag(imageToken(line))
		case constant.ProviderGitlab:
			inferred.Host, inferred.Repository = componentReference(line)
		case constant.ProviderHashicorp:
			inferred.Product = hashicorpProduct(path, line)
		case constant.ProviderTerraform, constant.ProviderOpentofu:
			inferred.Source = terraformSource(lines, target)
		}
		return inferred, true
	}
	return Inference{}, false
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

// hashicorpProduct names the product a hashicorp-routed line pins: terraform
// for a required_version constraint in a Terraform file, else the mise tool
// key, which doubles as the product slug.
func hashicorpProduct(path, line string) string {
	if matchPath(terraformGlob, path) {
		return terraformProduct
	}
	return toolKey(path, line)
}

// toolKey extracts the tool name from a line mise reads: the first
// whitespace-separated field in a .tool-versions file, else the mise TOML key.
func toolKey(path, line string) string {
	if matchPath(toolVersionsGlob, path) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return ""
		}
		return fields[0]
	}
	return miseKey(line)
}

// miseKey extracts the tool name from a mise configuration line, the quoted or
// bare TOML key before =, e.g. `terraform = "1.9.8"` -> "terraform".
func miseKey(line string) string {
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.Trim(strings.TrimSpace(key), `"'`)
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
	"rust":   {repository: "rust-lang/rust"},
}

// githubReference extracts the repository a line tracks on GitHub, and the
// tag-prefix its upstream tags carry: a uses: action reference, a .tofu file's
// required_version constraint, or a mise tool key.
func githubReference(path, line string) (string, string) {
	if repo := actionRepository(line); repo != "" {
		return repo, ""
	}
	if matchPath(tofuGlob, path) {
		return tofuTool.repository, tofuTool.tagPrefix
	}
	return miseTool(path, line)
}

// miseTool extracts the GitHub source a mise tool key tracks: a curated tool
// name from [miseGithubTools], a registry tool name from the generated
// miseRegistryTools, or a github: or ubi: backend key, e.g.
// `"ubi:owner/tool" = "1.2.3"` -> "owner/tool", dropping a trailing [option]
// qualifier. It returns empty strings when the key names no repository.
func miseTool(path, line string) (string, string) {
	key := toolKey(path, line)
	if tool, ok := miseGithubTools[key]; ok {
		return tool.repository, tool.tagPrefix
	}
	if repo, ok := miseRegistryTools[key]; ok {
		return repo, ""
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
