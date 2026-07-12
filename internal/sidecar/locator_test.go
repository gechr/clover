package sidecar_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/sidecar"
	"github.com/stretchr/testify/require"
)

func jqDirective(expr string) directive.Directive {
	return directive.Directive{Pairs: []directive.KV{{Key: "jq", Value: expr}}}
}

// TestLocatorSharedAcrossEntries confirms one Locator resolves many entries
// against the same document, successes and per-entry failures alike - the
// shape resolveSidecar drives.
func TestLocatorSharedAcrossEntries(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator(strings.Split(depsJSON, "\n"))

	line, err := locator.Locate(jqDirective(".dependencies.react"))
	require.NoError(t, err)
	require.Equal(t, 3, line)

	_, err = locator.Locate(jqDirective(".dependencies.nosuch"))
	require.EqualError(t, err, `"jq" locator key "nosuch" not found`)

	_, err = locator.Locate(jqDirective(".versions[5]"))
	require.EqualError(t, err, `"jq" locator index 5 out of range`)

	_, err = locator.Locate(jqDirective(".versions[-3]"))
	require.EqualError(t, err, `"jq" locator index -3 out of range`)

	line, err = locator.Locate(jqDirective(`.["$schema"]`))
	require.NoError(t, err)
	require.Equal(t, 11, line)
}

// TestLocatorFindAvoidsJSONParse confirms a find-only entry resolves without
// requiring the target to be JSON at all.
func TestLocatorFindAvoidsJSONParse(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator([]string{"# not json", "version = 1.2.3"})
	d := directive.Directive{Pairs: []directive.KV{{Key: "find", Value: "version = <version>"}}}

	line, err := locator.Locate(d)
	require.NoError(t, err)
	require.Equal(t, 1, line)
}

// TestLocatorNonJSONPerEntry confirms every jq entry against a non-JSON target
// reports the same requires-a-JSON-target error, not just the first.
func TestLocatorNonJSONPerEntry(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator([]string{"not json at all"})
	for range 2 {
		_, err := locator.Locate(jqDirective(".version"))
		require.EqualError(
			t,
			err,
			`"jq" locator requires a JSON target: invalid character 'o' in literal null (expecting 'u')`,
		)
	}
}

// TestLocatorTrailingContent confirms content after the top-level value is
// rejected, matching json.Unmarshal's strictness.
func TestLocatorTrailingContent(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator([]string{`{"a": "1"}`, "trailing"})
	_, err := locator.Locate(jqDirective(".a"))
	require.EqualError(
		t,
		err,
		`"jq" locator requires a JSON target: invalid character 't' after top-level value`,
	)
}

// TestLocatorSegmentKinds exercises every path segment shape gojq can emit -
// keys, indices (negative included), the root path, and nested arrays - plus
// the scalar kinds a locator can land on.
func TestLocatorSegmentKinds(t *testing.T) {
	t.Parallel()

	const doc = `{
  "matrix": [
    ["a", "b"],
    ["c", "d"]
  ],
  "count": 42,
  "ok": true,
  "none": null
}`
	lines := strings.Split(doc, "\n")

	tests := []struct {
		name string
		expr string
		want int
	}{
		{"root", ".", 0},
		{"nested array element", ".matrix[1][0]", 3},
		{"negative nested index", ".matrix[-1][-1]", 3},
		{"number scalar", ".count", 5},
		{"bool scalar", ".ok", 6},
		{"null scalar", ".none", 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line, err := sidecar.NewLocator(lines).Locate(jqDirective(tc.expr))
			require.NoError(t, err)
			require.Equal(t, tc.want, line)
		})
	}
}

// TestLocatorRootWithLeadingWhitespace confirms the root locator resolves to
// the root value's own line - not line 0 - when blank lines precede it, in
// both the offset index and the per-entry byte walk.
func TestLocatorRootWithLeadingWhitespace(t *testing.T) {
	t.Parallel()

	lines := []string{"", "", `"1.0.0"`}

	line, err := sidecar.NewLocator(lines).Locate(jqDirective("."))
	require.NoError(t, err)
	require.Equal(t, 2, line)

	ref, err := sidecar.ResolveJQLine([]byte(strings.Join(lines, "\n")), ".")
	require.NoError(t, err)
	require.Equal(t, 2, ref)
}

// TestLocatorSliceSegment confirms a slice path - which selects a range, not a
// single value - is rejected as a structural mismatch, as the byte walk always
// did.
func TestLocatorSliceSegment(t *testing.T) {
	t.Parallel()

	_, err := sidecar.NewLocator(strings.Split(depsJSON, "\n")).
		Locate(jqDirective(".versions[1:2]"))
	require.EqualError(t, err, `"jq" locator path does not match the JSON structure`)
}

// TestLocatorEscapedKeys confirms keys that need JSON escaping - quotes,
// backslashes, control characters, and non-ASCII - index and resolve
// consistently through the bracket-form locators Leaves emits for them.
func TestLocatorEscapedKeys(t *testing.T) {
	t.Parallel()

	const doc = `{
  "with \"quote\"": "1.0.0",
  "back\\slash": "2.0.0",
  "tab\there": "3.0.0",
  "naïve": "4.0.0"
}`
	lines := strings.Split(doc, "\n")

	tests := []struct {
		name string
		expr string
		want int
	}{
		{"quoted key", `.["with \"quote\""]`, 1},
		{"backslash key", `.["back\\slash"]`, 2},
		{"control char key", `.["tab\there"]`, 3},
		{"non-ASCII key", `.["naïve"]`, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line, err := sidecar.NewLocator(lines).Locate(jqDirective(tc.expr))
			require.NoError(t, err)
			require.Equal(t, tc.want, line)
		})
	}
}

// TestLocatorCompactDocument confirms a single-line document resolves every
// entry to line 0 - offsets differ, lines agree.
func TestLocatorCompactDocument(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator([]string{`{"a": "1", "b": {"c": "2"}, "d": ["3"]}`})
	for _, expr := range []string{".a", ".b.c", ".d[0]"} {
		line, err := locator.Locate(jqDirective(expr))
		require.NoError(t, err)
		require.Equal(t, 0, line, expr)
	}
}

// TestLocatorEmptyContainers confirms lookups into empty containers fail with
// the byte walk's own errors.
func TestLocatorEmptyContainers(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator([]string{`{"obj": {}, "arr": []}`})

	_, err := locator.Locate(jqDirective(".obj.missing"))
	require.EqualError(t, err, `"jq" locator key "missing" not found`)

	_, err = locator.Locate(jqDirective(".arr[0]"))
	require.EqualError(t, err, `"jq" locator index 0 out of range`)
}

// TestLocatorEmptyExpression confirms the empty-expression error survives the
// shared-locator path.
func TestLocatorEmptyExpression(t *testing.T) {
	t.Parallel()

	_, err := sidecar.NewLocator([]string{"{}"}).Locate(jqDirective(""))
	require.EqualError(t, err, `"jq" expression is empty`)
}

// TestLocatorNoLocator confirms an entry carrying neither find nor jq is a
// hard error.
func TestLocatorNoLocator(t *testing.T) {
	t.Parallel()

	d := directive.Directive{Pairs: []directive.KV{{Key: "provider", Value: "npm"}}}
	_, err := sidecar.NewLocator([]string{"{}"}).Locate(d)
	require.EqualError(t, err, `needs a "find" or "jq" locator`)
}

// TestLocatorEmptyTarget confirms an empty target is the non-JSON-target
// error, not a panic or a zero line.
func TestLocatorEmptyTarget(t *testing.T) {
	t.Parallel()

	_, err := sidecar.NewLocator(nil).Locate(jqDirective(".a"))
	require.EqualError(
		t,
		err,
		`"jq" locator requires a JSON target: unexpected end of JSON input`,
	)
}

// TestLocatorDuplicateKeySharedState confirms a duplicated key routes every
// entry - resolving and erroring alike - through the last-value-wins fallback
// without corrupting the shared locator.
func TestLocatorDuplicateKeySharedState(t *testing.T) {
	t.Parallel()

	const dup = `{
  "pkg": "1.0.0",
  "dep": { "v": "1.0.0" },
  "pkg": "2.0.0",
  "dep": { "v": "2.0.0" }
}`
	locator := sidecar.NewLocator(strings.Split(dup, "\n"))

	line, err := locator.Locate(jqDirective(".pkg"))
	require.NoError(t, err)
	require.Equal(t, 3, line, "the last pkg")

	line, err = locator.Locate(jqDirective(".dep.v"))
	require.NoError(t, err)
	require.Equal(t, 4, line, "v inside the last dep")

	_, err = locator.Locate(jqDirective(".nosuch"))
	require.EqualError(t, err, `"jq" locator key "nosuch" not found`)
}

// TestLocatorRecoversAfterErrors confirms failed lookups leave the shared
// index intact for later entries.
func TestLocatorRecoversAfterErrors(t *testing.T) {
	t.Parallel()

	locator := sidecar.NewLocator(strings.Split(depsJSON, "\n"))

	_, err := locator.Locate(jqDirective(".versions[]"))
	require.EqualError(t, err, "jq matched 2 values - narrow it")

	_, err = locator.Locate(jqDirective(".empty[]"))
	require.EqualError(t, err, "jq matched nothing")

	line, err := locator.Locate(jqDirective(".dependencies[\"left-pad\"]"))
	require.NoError(t, err)
	require.Equal(t, 4, line)
}
