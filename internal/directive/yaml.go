package directive

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// nullTag is the resolved tag yaml.v3 gives a null scalar, so `find:` and
// `find: null` are both recognized as the empty value a directive must not carry.
const nullTag = "!!null"

// ParseYAML decodes one YAML mapping node into a [Directive]. Each key maps to a
// directive key; a scalar value becomes one pair. A sequence value is accepted
// for a repeatable key (include, exclude), expanding to one pair per item, and
// for a CSV key (tags), joining the items into one comma-separated value. Source
// order is not preserved: canonical order is imposed on emit by [RenderYAML],
// never on parse. A non-mapping node, or a sequence on a key that is neither
// repeatable nor CSV, is a hard error.
func ParseYAML(node *yaml.Node) (Directive, error) {
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return Directive{}, fmt.Errorf("sidecar entry must be a YAML mapping")
	}

	var pairs []KV
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		switch value.Kind {
		case yaml.ScalarNode:
			if value.Value == "" || value.Tag == nullTag {
				return Directive{}, fmt.Errorf("%q has an empty value", key)
			}
			pairs = append(pairs, KV{Key: key, Value: value.Value})
		case yaml.SequenceNode:
			if !isRepeatable(key) && !isCSV(key) {
				return Directive{}, fmt.Errorf("%q does not accept a list value", key)
			}
			items := make([]string, 0, len(value.Content))
			for _, item := range value.Content {
				if item.Kind != yaml.ScalarNode {
					return Directive{}, fmt.Errorf("%q list items must be scalars", key)
				}
				items = append(items, item.Value)
			}
			if isCSV(key) {
				pairs = append(pairs, KV{Key: key, Value: strings.Join(items, ",")})
			} else {
				for _, item := range items {
					pairs = append(pairs, KV{Key: key, Value: item})
				}
			}
		case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
			return Directive{}, fmt.Errorf("%q has an unsupported YAML value", key)
		}
	}
	return Directive{Pairs: pairs}, nil
}

// RenderYAML serializes a directive to a YAML mapping node in canonical key
// order (so provider: leads), reusing the one [Reorder] the text codec calls and
// canonicalizing tags identically. Consecutive repeated keys collapse into a
// single key with a sequence value. Scalar quoting is left to the YAML encoder.
func RenderYAML(d Directive, providerKeys []string) *yaml.Node {
	d = CanonicalizeTags(Reorder(d, providerKeys))

	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i < len(d.Pairs); {
		key := d.Pairs[i].Key
		if !isRepeatable(key) {
			node.Content = append(node.Content, scalarNode(key), scalarNode(d.Pairs[i].Value))
			i++
			continue
		}

		// A repeatable key collapses its consecutive run into one sequence value;
		// a lone occurrence stays a scalar so the common case reads naturally.
		run := 1
		for i+run < len(d.Pairs) && d.Pairs[i+run].Key == key {
			run++
		}
		node.Content = append(node.Content, scalarNode(key))
		if run == 1 {
			node.Content = append(node.Content, scalarNode(d.Pairs[i].Value))
		} else {
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for _, kv := range d.Pairs[i : i+run] {
				seq.Content = append(seq.Content, scalarNode(kv.Value))
			}
			node.Content = append(node.Content, seq)
		}
		i += run
	}
	return node
}

// scalarNode builds a string scalar node, letting [yaml.Node.SetString] pick the
// tag and style so the encoder quotes the value only when YAML requires it.
func scalarNode(value string) *yaml.Node {
	node := new(yaml.Node)
	node.SetString(value)
	return node
}

// RenderYAMLList serializes entry mapping nodes (each from [RenderYAML]) as a
// YAML sequence document - the sidecar file's top-level shape. It is the codec's
// document writer, shared by annotate's generation and format's re-emit so both
// lay down byte-identical canonical output for the same entries.
func RenderYAMLList(entries []*yaml.Node) ([]byte, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: entries}
	return yaml.Marshal(seq)
}
