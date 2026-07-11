package sidecar

import (
	"bytes"
	"slices"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	xslices "github.com/gechr/x/slices"
	"gopkg.in/yaml.v3"
)

// Render serializes directive entries as a canonical YAML sidecar document. Each
// entry is rendered in canonical key order via the shared codec; keysFor supplies
// an entry's provider keys, kept a callback so this format-agnostic package never
// depends on the provider registry. It is the writer annotate generates new
// sidecars with - and the canonical form format re-emits toward.
func Render(entries []directive.Directive, keysFor func(provider string) []string) ([]byte, error) {
	nodes := xslices.Map(entries, func(d directive.Directive) *yaml.Node {
		name, _ := d.Get(constant.DirectiveProvider)
		return directive.RenderYAML(d, keysFor(name))
	})
	return directive.RenderYAMLList(nodes)
}

// Canonical is the outcome of re-emitting a sidecar: the canonical bytes, the
// unknown keys a prune removed, and whether the bytes differ from the input.
type Canonical struct {
	Content []byte
	Pruned  []string
	Changed bool
}

// Canonicalize re-emits a sidecar in canonical form: each entry's keys are
// reordered and its tags normalized, while every comment survives (the entry
// node is rebuilt in place and [carryComments] re-homes its own head, line, and
// foot comments plus any note on an individual key or value, and the document's
// own comments are kept by re-marshaling the parsed tree). With
// prune set, an entry's unknown keys are stripped; without it, an unknown key is
// rejected with an error so a stale key cannot pass a format gate. A structurally
// broken sidecar - one that does not parse, or whose entry is malformed - is left
// untouched (Changed false, no error): lint owns those diagnostics, not format.
func Canonicalize(
	data []byte,
	keysFor func(provider string) []string,
	prune bool,
) (Canonical, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Canonical{}, nil //nolint:nilerr // a broken sidecar is lint's to report
	}
	if len(doc.Content) == 0 {
		return Canonical{}, nil // an empty document has nothing to canonicalize
	}
	root := doc.Content[0]
	if root.Kind != yaml.SequenceNode {
		return Canonical{}, nil
	}

	var pruned []string
	for i, item := range root.Content {
		d, err := directive.ParseYAML(item)
		if err != nil {
			return Canonical{}, nil //nolint:nilerr // a malformed entry is lint's to report
		}
		name, _ := d.Get(constant.DirectiveProvider)
		keys := keysFor(name)
		if prune {
			var removed []string
			d, removed = d.PruneUnknownKeys(append(slices.Clone(keys), constant.DirectiveJQ))
			pruned = append(pruned, removed...)
		} else if err := d.CheckKeysSidecar(keys); err != nil {
			return Canonical{}, err
		}

		// Rebuild the entry in canonical order, carrying its comments across so a
		// note above or beside an entry - or any of its keys - survives the re-emit.
		node := directive.RenderYAML(d, keys)
		carryComments(item, node)
		root.Content[i] = node
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return Canonical{}, err
	}
	return Canonical{Content: out, Pruned: pruned, Changed: !bytes.Equal(out, data)}, nil
}

// Refresh re-emits a sidecar with selected entries patched in place. Each entry
// is parsed and located via its own locator; for one that locates, refresh may
// return a replacement directive (re-rendered in canonical order, carrying the
// entry's comments across - including any note on its keys). An entry that does
// not parse or does not locate is kept verbatim - so a force pass never deletes
// an unresolvable or hand-written entry it cannot reproduce - and the document's
// comments survive. The fresh entries are appended. lines is the located target's
// content, the source the locators resolve against.
func Refresh(
	data []byte,
	lines []string,
	keysFor func(provider string) []string,
	refresh func(line int, d directive.Directive) (directive.Directive, bool),
	fresh []directive.Directive,
) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	var root *yaml.Node
	if len(doc.Content) > 0 && doc.Content[0].Kind == yaml.SequenceNode {
		root = doc.Content[0]
	} else {
		root = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	}

	for i, item := range root.Content {
		d, err := directive.ParseYAML(item)
		if err != nil {
			continue // a malformed entry is kept verbatim; lint owns its diagnostics
		}
		line, err := Locate(lines, d)
		if err != nil {
			continue // an unresolvable entry is kept verbatim, never silently dropped
		}
		repl, ok := refresh(line, d)
		if !ok {
			continue
		}
		name, _ := repl.Get(constant.DirectiveProvider)
		node := directive.RenderYAML(repl, keysFor(name))
		carryComments(item, node)
		root.Content[i] = node
	}

	for _, d := range fresh {
		name, _ := d.Get(constant.DirectiveProvider)
		root.Content = append(root.Content, directive.RenderYAML(d, keysFor(name)))
	}
	return yaml.Marshal(&doc)
}

// carryComments transplants every comment from a parsed sidecar entry onto its
// canonically re-rendered replacement. The entry's own head, line, and foot
// comments move across, and so does any note attached to a key or value node,
// re-homed by key name because the canonical reorder reshuffles the pairs (a
// position copy would land a comment on the wrong field). A key dropped by a
// prune has nowhere to land, so its comment is dropped with it.
func carryComments(src, dst *yaml.Node) {
	copyComments(src, dst)

	type notes struct{ key, value *yaml.Node }
	byKey := make(map[string]notes)
	for i := 0; i+1 < len(src.Content); i += 2 {
		byKey[src.Content[i].Value] = notes{src.Content[i], src.Content[i+1]}
	}
	for i := 0; i+1 < len(dst.Content); i += 2 {
		n, ok := byKey[dst.Content[i].Value]
		if !ok {
			continue
		}
		copyComments(n.key, dst.Content[i])
		copyComments(n.value, dst.Content[i+1])
	}
}

// copyComments moves the three comment fields from one node onto another.
func copyComments(src, dst *yaml.Node) {
	dst.HeadComment = src.HeadComment
	dst.LineComment = src.LineComment
	dst.FootComment = src.FootComment
}
