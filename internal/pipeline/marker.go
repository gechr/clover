package pipeline

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/vcs"
)

// bareID returns the user-written id from a namespaced key (root + nsSep + id),
// for display in messages. A key without the separator is returned unchanged.
func bareID(key string) string {
	if _, id, ok := strings.Cut(key, nsSep); ok {
		return id
	}
	return key
}

// nsSep joins a repository root and an id into a namespaced key. It is a NUL so
// it can never appear in either part.
const nsSep = "\x00"

// Marker is a directive bound to the line it governs, classified as a producer
// (resolves from an upstream provider) or a follower (reuses another marker's
// result), with id and from namespaced to the file's repository so the same id
// in different repositories does not clash.
type Marker struct {
	File      string
	Line      int // 0-based index of the directive's comment line
	Target    int // 0-based index of the line it rewrites
	Directive directive.Directive
	Provider  string   // provider name; follow for a follower
	ID        string   // namespaced producer id, or ""
	From      string   // namespaced follow source, or ""
	Value     string   // value kind a follower projects
	Select    string   // old/new snapshot a follower reads
	Tags      []string // labels for --tags filtering, in source order
}

// IsFollower reports whether the marker reuses another marker's result rather
// than resolving from an upstream provider.
func (m Marker) IsFollower() bool { return m.Provider == constant.ProviderFollow }

// Markers binds every directive found in file to its target line, classifies it,
// and namespaces its id and from by the file's repository root.
func Markers(file scan.File, resolver *vcs.Resolver) []Marker {
	root := resolver.Root(file.Path)
	markers := make([]Marker, 0, len(file.Found))
	for _, found := range file.Found {
		markers = append(markers, bind(file.Path, root, found))
	}
	return markers
}

// bind turns a located directive into a Marker. The target defaults to the next
// line; offset/range targeting is a later addition. An omitted provider means
// the marker follows another (provider= follow).
func bind(path, root string, found scan.Located) Marker {
	d := found.Directive

	provider := value(d, constant.DirectiveProvider)
	if provider == "" {
		provider = constant.ProviderFollow
	}

	return Marker{
		File:      path,
		Line:      found.Line,
		Target:    found.Line + 1,
		Directive: d,
		Provider:  provider,
		ID:        namespace(root, value(d, constant.DirectiveID)),
		From:      namespace(root, value(d, constant.DirectiveFrom)),
		Value:     value(d, constant.DirectiveValue),
		Select:    value(d, constant.DirectiveSelect),
		Tags:      d.CSV(constant.DirectiveTags),
	}
}

// namespace prefixes id with the repository root. An empty id stays empty.
func namespace(root, id string) string {
	if id == "" {
		return ""
	}
	return root + nsSep + id
}

// value returns a directive key's value, or "" when absent.
func value(d directive.Directive, key string) string {
	v, _ := d.Get(key)
	return v
}
