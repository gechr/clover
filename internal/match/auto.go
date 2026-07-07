package match

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
)

// Inference is what auto-detection resolved for a `provider=auto` marker from
// its target line: the real provider plus any provider parameters readable from
// the line. Empty parameter fields mean the line did not carry that detail.
type Inference struct {
	Provider   string
	Registry   string
	Repository string
}

// Infer resolves the provider for an `auto` marker from its file path and target
// line, reusing the dispatch routes: the first route whose path and line match
// (ignoring its provider guard, which is the answer) names the provider. It also
// reads the provider's parameters from the line - the repository from a GitHub
// Actions pin, the registry and repository from a container image reference - so
// a bare `provider=auto` needs no further keys. It returns ok=false when nothing
// matches, leaving the marker for the caller to reject.
func Infer(path, line string) (Inference, bool) {
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
			inferred.Repository = actionRepository(line)
		case constant.ProviderDocker:
			inferred.Registry, inferred.Repository = imageReference(line)
		}
		return inferred, true
	}
	return Inference{}, false
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
