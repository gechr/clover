package match

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
)

// Infer resolves the provider for an `auto` marker from its file path and target
// line, reusing the dispatch routes: the first route whose path and line match
// (ignoring its provider guard, which is the answer) names the provider. For a
// GitHub Actions pin it also reads the repository from the uses: reference, so a
// bare `provider=auto` needs no `repository=`. It returns ok=false when nothing
// matches, leaving the marker for the caller to reject.
func Infer(path, line string) (string, string, bool) {
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
		repository := ""
		if c.provider == constant.ProviderGithub {
			repository = actionRepository(line)
		}
		return c.provider, repository, true
	}
	return "", "", false
}

// actionRepository extracts the owner/repo from a GitHub Actions uses: pin,
// e.g. "uses: gechr/actions/.github/workflows/lint.yaml@<sha>" -> "gechr/actions".
// It returns "" when the line is not an owner/repo reference.
func actionRepository(line string) string {
	_, after, ok := strings.Cut(line, "uses:")
	if !ok {
		return ""
	}
	ref := strings.TrimSpace(after)
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
