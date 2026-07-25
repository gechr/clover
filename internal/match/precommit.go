package match

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
	"gopkg.in/yaml.v3"
)

// preCommitForges maps a forge host to the provider that tracks its tags. A
// pre-commit repo: is any git URL, so the host is the only signal for which
// upstream a rev belongs to - the same tag syntax serves all of them.
//
// Only hosts a provider reaches with no extra directive key appear. Codeberg is
// the gitea provider's default flavor, so it needs none; the other two Gitea
// flavors (code.forgejo.org, gitea.com) would each need a flavor key that
// [Inference] has no field for, so a rev on one is declined rather than
// resolved against the wrong host.
var preCommitForges = map[string]string{
	"codeberg.org": constant.ProviderGitea,
	"github.com":   constant.ProviderGithub,
	"gitlab.com":   constant.ProviderGitlab,
}

// inferPreCommitRev reads the repository a .pre-commit-config.yaml rev pins,
// and the provider whose forge hosts it. Both come from the repo: URL on a
// sibling line of the same repos entry, so the target line alone names nothing.
//
// It declines - naming no provider - for a repo: that is not a forge Clover
// reaches: the local and meta pseudo-repositories pre-commit uses for
// repository-local hooks, and any self-hosted forge. Guessing a provider from an
// unrecognized host would resolve the rev against a forge that never published
// it.
func inferPreCommitRev(s subject) Inference {
	host, repository, ok := forgeReference(preCommitRepo(s.lines, s.target))
	if !ok {
		return Inference{}
	}
	// A hostname is case-insensitive, so the lookup folds case to match the
	// semantics of the thing it keys on: GitHub.com is the same forge.
	name, found := preCommitForges[strings.ToLower(host)]
	if !found {
		return Inference{}
	}
	return Inference{Provider: name, Repository: repository}
}

// preCommitRepo extracts the repo: URL governing the rev scalar at
// lines[target], parsing the whole file as YAML and locating the repos entry
// whose rev sits on that line. Reading the node tree rather than scanning
// neighbouring lines is what makes the correlation exact: pre-commit configs
// vary their list indentation freely, and a rev may precede or follow the
// hooks: it shares an entry with. It returns "" when the file does not parse or
// the line belongs to no repos entry.
func preCommitRepo(lines []string, target int) string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &doc); err != nil {
		return ""
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return ""
	}
	repos := mappingValue(root, "repos")
	if repos == nil || repos.Kind != yaml.SequenceNode {
		return ""
	}
	for _, entry := range repos.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if rev := mappingValue(entry, "rev"); rev != nil && rev.Line == target+1 {
			return scalarValue(mappingValue(entry, "repo"))
		}
	}
	return ""
}

// gitSuffix is the optional extension a clone URL carries, dropped so the same
// repository spelled with and without it reaches one upstream.
const gitSuffix = ".git"

// forgeReference splits a git clone URL into its forge host and the repository
// path beneath it, e.g. "https://github.com/owner/name.git" ->
// ("github.com", "owner/name"). It accepts the scheme form any forge serves over
// HTTP and the scp-like SSH form (git@host:owner/name), the two spellings a
// pre-commit repo: is written in.
//
// The path must carry at least an owner and a name; a bare host, a single
// segment, or a value that is no URL at all (local, meta) is not a reference.
// Deeper paths are preserved for the forges that nest projects in subgroups.
func forgeReference(raw string) (string, string, bool) {
	rest, scheme := raw, false
	if _, after, found := strings.Cut(raw, "://"); found {
		rest, scheme = after, true
	}
	if _, after, found := strings.Cut(rest, "@"); found {
		rest = after // a userinfo prefix, or the scp-like form's git@
	}
	if !scheme {
		// An scp-like remote separates host from path with a colon rather than a
		// slash, so normalizing it lets one split serve both spellings. A URL's
		// colon introduces a port instead, which must survive to be dropped below
		// rather than becoming the first path segment.
		rest = strings.Replace(rest, ":", "/", 1)
	}

	host, path, ok := strings.Cut(rest, "/")
	if !ok || host == "" {
		return "", "", false
	}
	host, _, _ = strings.Cut(host, ":") // a port is not part of the forge's identity
	path = strings.Trim(strings.TrimSuffix(strings.TrimSuffix(path, "/"), gitSuffix), "/")
	if strings.Count(path, "/") < 1 {
		return "", "", false
	}
	return host, path, true
}
