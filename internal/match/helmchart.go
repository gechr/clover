package match

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// helmDependency extracts the chart name and repository governing the Chart.yaml
// dependencies version scalar at lines[target], parsing the whole file as YAML
// and locating the dependency entry whose version sits on that line. The line
// alone names nothing - the chart name and repository live on sibling fields of
// the entry - so this is a context-aware inference like terraformSource. It
// returns empty strings when the file does not parse or the line belongs to no
// dependencies entry (the chart's own top-level version, for instance, pins the
// chart itself, not a dependency). It returns the chart name and repository, in
// that order.
func helmDependency(lines []string, target int) (string, string) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &doc); err != nil {
		return "", ""
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return "", ""
	}
	deps := mappingValue(root, "dependencies")
	if deps == nil || deps.Kind != yaml.SequenceNode {
		return "", ""
	}
	for _, entry := range deps.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		version := mappingValue(entry, "version")
		if version == nil || version.Line != target+1 {
			continue
		}
		return scalarValue(mappingValue(entry, "name")),
			scalarValue(mappingValue(entry, "repository"))
	}
	return "", ""
}

// documentRoot unwraps a parsed document node to its single content node, the
// mapping a Chart.yaml's keys hang off. It returns nil when the document is
// empty.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	return doc.Content[0]
}

// mappingValue returns the value node paired with key in a mapping node, or nil
// when the key is absent. A mapping's content alternates key, value.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// scalarValue returns a scalar node's value, or "" when the node is nil or not a
// scalar (a source only ever names a plain string).
func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}
