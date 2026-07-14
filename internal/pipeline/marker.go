package pipeline

import (
	"strings"

	"github.com/gechr/clover/internal/comment"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/infer"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/scan"
	"github.com/gechr/clover/internal/vcs"
	xslices "github.com/gechr/x/slices"
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
	Line      int   // 0-based index of the directive's comment line
	Target    int   // 0-based index of the line it rewrites; -1 when resolution failed
	TargetErr error // why the target line could not be resolved
	Directive directive.Directive
	Provider  string   // provider name; follow for a follower
	ID        string   // namespaced producer id, or ""
	From      string   // namespaced follow source, or ""
	Value     string   // value kind a follower projects
	Select    string   // old/new snapshot a follower reads
	Tags      []string // labels for --tags filtering, in source order
	Sidecar   bool     // the directive came from a YAML sidecar
	Inferred  bool     // synthesized by run --infer; no written directive exists
}

// IsFollower reports whether the marker reuses another marker's result rather
// than resolving from an upstream provider.
func (m Marker) IsFollower() bool { return m.Provider == constant.ProviderFollow }

// Markers binds every directive found in file to its target line, classifies it,
// and namespaces its id and from by the file's repository root.
func Markers(file scan.File, resolver *vcs.Resolver) []Marker {
	root := resolver.Root(file.Path)
	return xslices.Map(file.Found, func(found scan.Located) Marker {
		return bind(file, root, found)
	})
}

// bind turns a located directive into a Marker. The target is the next line, a
// target= anchor's first match below the comment, or a sidecar entry's already-
// resolved line - scan.Located.Target decides. An omitted provider means the
// marker follows another (provider= follow); provider=auto is resolved from the
// target line.
func bind(file scan.File, root string, found scan.Located) Marker {
	d := directive.CanonicalizeAliases(found.Directive)
	target, targetErr := found.Target(file.Lines)
	if targetErr != nil {
		target = -1 // no line to rewrite; validation reports targetErr
	}

	provider := value(d, constant.DirectiveProvider)
	switch provider {
	case "":
		provider = constant.ProviderFollow
	case constant.ProviderAuto:
		provider, d = inferParams(file, target, d)
	}

	// A tool key's upstream tags may carry a prefix (erlang tags OTP-27.2), so
	// bind appends the rule the tool map records, unless the directive pins its
	// own. The provider itself only resolves the repository.
	if provider == constant.ProviderGithub {
		if _, prefix, ok := match.LookupTool(value(d, constant.DirectiveTool)); ok {
			d = appendParam(d, constant.RuleTagPrefix, prefix)
		}
	}

	return Marker{
		File:      file.Path,
		Line:      found.Line,
		Target:    target,
		TargetErr: targetErr,
		Directive: d,
		Provider:  provider,
		ID:        namespace(root, value(d, constant.DirectiveID)),
		From:      namespace(root, value(d, constant.DirectiveFrom)),
		Value:     value(d, constant.DirectiveValue),
		Select:    value(d, constant.DirectiveSelect),
		Tags:      d.CSV(constant.DirectiveTags),
		Sidecar:   found.Sidecar,
	}
}

// inferParams resolves a provider=auto marker from its target line, returning
// the detected provider and the directive with any inferred params (e.g.
// repository) appended. When nothing matches, the provider stays auto so
// resolution rejects it with a clear "unknown provider" error.
func inferParams(file scan.File, target int, d directive.Directive) (string, directive.Directive) {
	if target < 0 || target >= len(file.Lines) {
		return constant.ProviderAuto, d
	}
	inferred, ok := match.Infer(file.Path, file.Lines, target)
	if !ok {
		return constant.ProviderAuto, d
	}
	d = appendParam(d, constant.DirectiveChart, inferred.Chart)
	d = appendParam(d, constant.DirectiveRegistry, inferred.Registry)
	d = appendParam(d, constant.DirectiveRepository, inferred.Repository)
	d = appendParam(d, constant.DirectiveHost, inferred.Host)
	d = appendParam(d, constant.DirectivePackage, inferred.Package)
	d = appendParam(d, constant.DirectiveProduct, inferred.Product)
	d = appendParam(d, constant.DirectiveSource, inferred.Source)
	d = appendParam(d, constant.RuleTagPrefix, inferred.TagPrefix)
	d = appendParam(d, constant.DirectiveTrack, inferred.Track)
	return inferred.Provider, d
}

// appendParam returns d with key=value appended, unless the value is empty or
// the key is already present - an explicit value always wins over an inferred one.
func appendParam(d directive.Directive, key, value string) directive.Directive {
	if value == "" || d.Has(key) {
		return d
	}
	pairs := append([]directive.KV{}, d.Pairs...)
	pairs = append(pairs, directive.KV{Key: key, Value: value})
	return directive.Directive{Pairs: pairs}
}

// InferredMarkers synthesizes a provider=auto marker for every ungoverned line
// auto-detection recognizes and can resolve offline, so run --infer updates a
// codebase carrying no directives at all. The gate is the one annotate uses
// ([infer.Recognize]), so a recognized line that would not resolve is skipped
// silently rather than failing the run; governed lines (a written directive's
// target) and comment lines are never doubled up.
func InferredMarkers(file scan.File, governed map[int]bool) []Marker {
	syntax := comment.For(file.Path)
	recognizer := infer.NewRecognizer(file.Path)
	var markers []Marker
	for i, line := range file.Lines {
		if governed[i] || file.Ignored.Contains(i) || syntax.IsComment(line) {
			continue
		}
		if _, reason, ok := recognizer.Recognize(file.Lines, i); !ok || reason != "" {
			continue
		}
		d := directive.Directive{Pairs: []directive.KV{
			{Key: constant.DirectiveProvider, Value: constant.ProviderAuto},
		}}
		provider, d := inferParams(file, i, d)
		markers = append(markers, Marker{
			File:      file.Path,
			Line:      i,
			Target:    i,
			Directive: d,
			Provider:  provider,
			Inferred:  true,
		})
	}
	return markers
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
