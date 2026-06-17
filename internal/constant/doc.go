// Package constant holds clover's shared string vocabulary - the literal
// spellings of directive grammar that appear across more than one package
// (constraint keywords now; provider names, source selectors, and value kinds
// as they land). Centralising them turns a misspelling into a compile error
// rather than a silent mismatch between the parser, formatter, providers, and
// CLI. It is a leaf: it imports nothing, so any package may depend on it.
package constant
