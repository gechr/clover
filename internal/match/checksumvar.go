package match

import (
	"path"
	"regexp"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/pattern"
	xstrings "github.com/gechr/x/strings"
)

// The fragments the checksum-variable routes and the pairing below are both
// built from, so the route that claims a line and the inference that reads its
// sibling can never disagree about the shape.
//
// checksumVarSuffixes are the spellings the trailing segment is written with,
// longest first so GO_SHA256SUM reads as GO rather than GO_SHA256. sha256Hex is
// the value, and it is the guard the shape rests on: a name alone would claim a
// line carrying anything at all, while a 64-character hex run is the sum itself,
// so a 40-character commit, a URL, or a bare filename is declined without
// consulting the name. It is also what makes the loosest suffixes safe to
// accept - _SHA usually names a commit, and a line where it does cannot match.
const (
	checksumVarSuffixes = `(?:SHA256SUM|SHA256|CHECKSUM|SUM|SHA)`
	sha256Hex           = `[0-9a-fA-F]{64}`
)

// checksumVarName matches the name of a checksum variable, capturing the tool
// part. The prefix is lazy so the longest suffix wins: GO_SHA256SUM captures GO,
// where a greedy prefix would capture GO_SHA256 and place no tool at all.
var checksumVarName = regexp.MustCompile(`\b([A-Z][A-Z0-9_]*?)_` + checksumVarSuffixes + `\b`)

// checksumVarAssign matches a whole checksum-variable assignment in either
// spelling a route claims - a Dockerfile ARG/ENV or a YAML mapping value - so
// the pairing walk recognizes a sibling on a line it never matched itself.
var checksumVarAssign = regexp.MustCompile(
	`\b[A-Z][A-Z0-9_]*?_` + checksumVarSuffixes + `\b\s*[=:\s]\s*["']?` + sha256Hex + `\b`,
)

// versionVarAssign is the same for the other half of a pair: a <TOOL>_VERSION
// name followed by a version-shaped value.
var versionVarAssign = regexp.MustCompile(
	`\b[A-Z][A-Z0-9_]*_VERSION\b\s*[=:\s]\s*["']?v?\d`,
)

// checksumPair is a <TOOL>_VERSION variable paired with the sibling checksum
// variable naming the same tool: the id the producer publishes under, the asset
// filename the follower sources its sum from, and the two line indexes. Both
// inferences read the same pairing, so a producer only ever earns an id when the
// follower that needs it would also be annotated, and vice versa.
type checksumPair struct {
	id      string
	pattern string
}

// pairChecksumVar finds the pairing the target line belongs to, reading whether
// it is the version half or the checksum half from the line itself. It returns
// false unless every part of the pairing holds:
//
//   - the prefixes match exactly, and each half appears exactly once. A repeated
//     prefix is ambiguous (which sum belongs to which pin?), and a multi-arch
//     Dockerfile spelling one sum per platform names each after the platform, so
//     its prefix places no tool and it declines here anyway.
//   - the prefix names a tool Clover tracks, on the same terms as the version
//     variable itself. The follower does not need the tool to resolve its own
//     line, but a producer that auto-detection would decline is one no `from=`
//     may point at.
//   - the file names the asset the sum belongs to (see [checksumAsset]).
func pairChecksumVar(s subject) (checksumPair, bool) {
	prefix, ok := checksumVarPrefix(s.line())
	if !ok {
		return checksumPair{}, false
	}
	if toolInference(xstrings.Slug(prefix)).Provider == "" {
		return checksumPair{}, false
	}
	if !soleMatch(s.lines, prefix+"_VERSION", versionVarAssign) ||
		!soleChecksumMatch(s.lines, prefix) {
		return checksumPair{}, false
	}
	asset := checksumAsset(s.lines, prefix+"_VERSION")
	if asset == "" {
		return checksumPair{}, false
	}
	return checksumPair{id: xstrings.Slug(prefix), pattern: asset}, true
}

// checksumVarPrefix reads the tool prefix a pair is keyed by from either half's
// line: the name before _VERSION, or the name before a checksum suffix. It
// returns false for a line that is neither, or whose value has the wrong shape
// for the half it names.
func checksumVarPrefix(line string) (string, bool) {
	if checksumVarAssign.MatchString(line) {
		if m := checksumVarName.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}
	if versionVarAssign.MatchString(line) {
		if m := versionVarName.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// soleMatch reports whether exactly one line both names name and matches shape.
// The name test is what scopes the count to one pair: another tool's variable
// matches the same shape and must not make its neighbour look ambiguous.
func soleMatch(lines []string, name string, shape *regexp.Regexp) bool {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, name) && shape.MatchString(line) {
			n++
		}
	}
	return n == 1
}

// soleChecksumMatch is [soleMatch] for the checksum half, which has no single
// name to test: the suffix varies, so the prefix is read back out of each
// matching line instead.
func soleChecksumMatch(lines []string, prefix string) bool {
	n := 0
	for _, line := range lines {
		if !checksumVarAssign.MatchString(line) {
			continue
		}
		if m := checksumVarName.FindStringSubmatch(line); m != nil && m[1] == prefix {
			n++
		}
	}
	return n == 1
}

// urlMarker is what a field must carry to be read as a download of the asset the
// sum belongs to. A bare path would let an unrelated mkdir or install line name
// the asset (`mkdir -p /opt/go$GO_VERSION`); a scheme means the file is fetched.
const urlMarker = "://"

// shellTrailers are the characters a URL is followed by in a shell command or a
// YAML scalar, trimmed off a field before its last segment is read.
const shellTrailers = "\"'`\\;|&()<>,"

// urlSuffixes are the URL parts that follow the path and so are not part of the
// filename: a query and a fragment. Both are cut before the last segment is read,
// since neither belongs in a pattern and `?` is a glob metacharacter that would
// match where it was never meant to. Cutting them can leave a basename with no
// <version> in it - the asset named inside a query string - which then declines,
// the right answer for a URL whose filename is not in its path.
var urlSuffixes = []string{"?", "#"}

// checksumAsset derives the filename a follower selects with `pattern=` by
// reading the line that downloads the asset, which is the only place in the file
// that names it. The version variable's own reference is what identifies it, and
// substituting the reference for <version> is what turns a URL into a pattern:
//
//	ARG GO_VERSION=1.24.0
//	RUN curl -O https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
//	  -> go<version>.linux-amd64.tar.gz
//
// It returns "" rather than guessing, and the pairing then declines the whole
// shape, whenever the file does not settle the question:
//
//   - no downloading line references the variable at all. A sum whose asset is
//     never named cannot be sourced, so the annotation would be written only to
//     fail on the next run.
//   - the filename still carries a variable after the substitution, which is how
//     a multi-arch build spells it (go<version>.linux-${TARGETARCH}.tar.gz). The
//     sum on that line belongs to one platform and Clover cannot tell which, so
//     pinning either one is a coin flip.
//   - two downloading lines disagree about the filename. Ambiguity is refused on
//     the same grounds a repeated prefix is.
func checksumAsset(lines []string, versionVar string) string {
	ref := versionVarRef(versionVar)
	placeholder := pattern.TokenVersion.Placeholder()

	found := ""
	for _, line := range lines {
		if !ref.MatchString(line) {
			continue
		}
		// Substituting before splitting is what lets an Actions expression
		// (${{ env.GO_VERSION }}) be read at all: the spaces that would otherwise
		// split the URL into three fields are inside the reference.
		for field := range strings.FieldsSeq(ref.ReplaceAllString(line, placeholder)) {
			if !strings.Contains(field, urlMarker) {
				continue
			}
			trimmed := strings.Trim(field, shellTrailers)
			for _, suffix := range urlSuffixes {
				trimmed, _, _ = strings.Cut(trimmed, suffix)
			}
			name := path.Base(trimmed)
			if !strings.Contains(name, placeholder) || strings.ContainsAny(name, "${}") {
				continue
			}
			if found != "" && found != name {
				return "" // two downloads disagree about the asset
			}
			found = name
		}
	}
	return found
}

// versionVarRef matches the spellings a line references a version variable by: a
// shell expansion in either brace form, and the GitHub Actions expression a
// workflow reaches an env: value through.
func versionVarRef(name string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(name)
	return regexp.MustCompile(
		`\$\{\{[^}]*\b` + quoted + `\b[^}]*\}\}|\$\{` + quoted + `\}|\$` + quoted + `\b`,
	)
}

// inferChecksumVariable reads the follower a <TOOL>_SHA256 variable stands for -
// a Dockerfile `ARG GO_SHA256=…`, a workflow env: value - by pairing it with the
// sibling <TOOL>_VERSION variable that pins the version the sum belongs to.
//
// It resolves to a follower rather than an upstream of its own, which is what
// keeps the sum and the version coherent: the sum is refreshed only when the
// version it follows actually changed, so the two can never step apart, and a
// re-published artifact cannot move a sum under an unchanged version.
func inferChecksumVariable(s subject) Inference {
	pair, ok := pairChecksumVar(s)
	if !ok {
		return Inference{}
	}
	return Inference{
		Provider: constant.ProviderFollow,
		From:     pair.id,
		Value:    constant.ValueSha256,
		Pattern:  pair.pattern,
	}
}
