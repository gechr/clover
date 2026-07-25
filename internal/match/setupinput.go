package match

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// matrixKey is the mapping key introducing a build matrix, in both the GitHub
// Actions (strategy: matrix:) and GitLab CI (parallel: matrix:) spellings.
const matrixKey = "matrix"

// inferSetupInput wraps a setup input's inference with the one thing its line
// cannot tell it: whether the pin is a build-matrix entry rather than a
// toolchain pin.
//
// A matrix lists the versions a project supports, so its oldest entry is
// deliberate and bumping it silently drops support. The route's line pattern
// already refuses the flow-list spelling (python-version: ['3.9', '3.14']), but
// an include-style matrix spells the identical fact as a plain scalar:
//
//	matrix:
//	  include:
//	    - toxenv: py39
//	      python-version: '3.9'
//
// which is indistinguishable from a step input on the line alone. So the shape
// is recognized by line and refused by context.
//
// It declines rather than requiring a with: or setup-action ancestor, which
// would be the obvious inverse. Requiring one would refuse the same pin written
// as a workflow env: value or a CI variable - the identical fact spelled
// elsewhere, and one this detection deliberately resolves. Declining under
// matrix: removes only the hazard.
//
// The refusal binds auto-detection alone. An explicit provider marker on a
// matrix line still dispatches through the smart rewriter, so a user who means
// a single-entry matrix to track upstream keeps a spelling that works.
func inferSetupInput(inner inferFunc) inferFunc {
	return func(s subject) Inference {
		if underMatrix(s.lines, s.target) {
			return Inference{}
		}
		return inner(s)
	}
}

// underMatrix reports whether lines[target] sits inside the value of a mapping
// key named matrix, parsing the whole file as YAML. It reports false when the
// file does not parse: a document Clover cannot read is not one it can claim a
// matrix in, and the line pattern has already refused the spellings that make a
// matrix obvious.
func underMatrix(lines []string, target int) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &doc); err != nil {
		return false
	}
	root := documentRoot(&doc)
	if root == nil {
		return false
	}
	return matrixScoped(root, target+1, false)
}

// matrixScoped reports whether the 1-based line falls within the value of a
// matrix key, walking node and carrying whether a matrix key has been entered.
// A mapping's key and its value share a line, so testing the value nodes it
// descends into reaches the target either way.
func matrixScoped(node *yaml.Node, line int, inMatrix bool) bool {
	if inMatrix && node.Line == line {
		return true
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if matrixScoped(value, line, inMatrix || key.Value == matrixKey) {
				return true
			}
		}
		return false
	}
	// A sequence carries no keys of its own, so it passes the matrix scope
	// through to its items. Any other kind is a leaf the line test above covers.
	for _, item := range node.Content {
		if matrixScoped(item, line, inMatrix) {
			return true
		}
	}
	return false
}
