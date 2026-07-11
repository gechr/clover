package sidecar

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/pattern"
	xslices "github.com/gechr/x/slices"
	"gopkg.in/yaml.v3"
)

// suffixYAML and suffixYML are the names that make a file a sidecar. They mirror
// the project config names (config.projectFileNames) used as suffixes, so the
// bare .clover.yaml / .clover.yml configs are never themselves sidecars.
const (
	suffixYAML = ".clover.yaml"
	suffixYML  = ".clover.yml"
)

// suffixes lists the sidecar name suffixes in precedence order: .yaml wins over
// .yml, mirroring the first-found ordering of config.load.
var suffixes = []string{suffixYAML, suffixYML}

// Entry is one sidecar list item: its directive, the 1-based source line it was
// written on (for diagnostics), and any error from decoding that single entry.
type Entry struct {
	Directive directive.Directive
	Line      int
	Err       error
}

// Target reports the target file name a sidecar name governs: name with the
// .clover.yaml / .clover.yml suffix removed. ok is false when name is not a
// sidecar name, or when removing the suffix leaves an empty base - the bare
// .clover.yaml / .clover.yml configs, which are config, never sidecars.
func Target(name string) (string, bool) {
	for _, suffix := range suffixes {
		if base, ok := strings.CutSuffix(name, suffix); ok && base != "" {
			return base, true
		}
	}
	return "", false
}

// Names returns the candidate sidecar file names for target, in precedence
// order (.yaml before .yml), so a caller can probe for an existing sidecar or
// resolve which one wins when both are present.
func Names(target string) []string {
	return xslices.Map(suffixes, func(suffix string) string {
		return target + suffix
	})
}

// Entries decodes a sidecar document into one entry per top-level list item. The
// document must be a YAML sequence; each item is decoded with directive.ParseYAML,
// and a single malformed item is reported in its own Entry.Err rather than
// failing the whole document. A document that is not a sequence is a hard error.
func Entries(data []byte) ([]Entry, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	root := &doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil, nil // an empty sidecar has no entries
		}
		root = doc.Content[0]
	}
	if root.Kind == 0 {
		return nil, nil // empty or whitespace-only input parses to no node
	}
	if root.Kind != yaml.SequenceNode {
		return nil, errors.New("sidecar must be a YAML list of entries")
	}

	entries := xslices.Map(root.Content, func(item *yaml.Node) Entry {
		d, err := directive.ParseYAML(item)
		return Entry{Directive: d, Line: item.Line, Err: err}
	})
	return entries, nil
}

// Locate resolves a sidecar entry's target line within lines via its locator. An
// entry must carry find or jq (neither present is a hard error). jq resolves the
// line by JSON path - robust against a duplicated version string or reordered
// keys - and a composing find then refines the region within that line at rewrite
// time; find alone scans for the single line whose content matches a glob or
// /regex/.
func Locate(lines []string, d directive.Directive) (int, error) {
	jqExpr, hasJQ := d.Get(constant.DirectiveJQ)
	find, hasFind := d.Get(constant.DirectiveFind)
	switch {
	case hasJQ:
		// jq selects the line; a composing find refines the region within it at
		// rewrite time, since the rewriter reads the resolved line's directive.
		return locateJQ(lines, jqExpr)
	case hasFind:
		return locateFind(lines, find)
	default:
		return 0, fmt.Errorf(
			"needs a %q or %q locator",
			constant.DirectiveFind,
			constant.DirectiveJQ,
		)
	}
}

// locateJQ resolves a jq locator to a line. The lossless LF-normalized lines are
// rejoined into the byte source the path walk descends, so offsets map back to
// line indices consistently with scan's split.
func locateJQ(lines []string, expr string) (int, error) {
	if expr == "" {
		return 0, fmt.Errorf("%q expression is empty", constant.DirectiveJQ)
	}
	return resolveJQLine([]byte(strings.Join(lines, "\n")), expr)
}

// locateFind returns the single line index whose content matches find, treating
// the pattern as a substring match (the same unanchored regex the find/replace
// rewriter uses). Zero or multiple matches is a hard error - the design fails
// loud rather than guessing which line was meant.
func locateFind(lines []string, find string) (int, error) {
	if find == "" {
		return 0, fmt.Errorf("%q pattern is empty", constant.DirectiveFind)
	}
	pat, err := pattern.Compile(find)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", constant.DirectiveFind, err)
	}

	re := pat.Regexp()
	match, count := 0, 0
	for i, line := range lines {
		if re.MatchString(line) {
			if count == 0 {
				match = i
			}
			count++
		}
	}
	switch count {
	case 0:
		return 0, errors.New("find matched no line")
	case 1:
		return match, nil
	default:
		return 0, fmt.Errorf("find matched %d lines - make it more specific", count)
	}
}
