package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// mappingKeys returns the keys of a mapping node in order (every even child).
func mappingKeys(node *yaml.Node) []string {
	keys := make([]string, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keys = append(keys, node.Content[i].Value)
	}
	return keys
}

func pairKeys(pairs []directive.KV) []string {
	keys := make([]string, len(pairs))
	for i, kv := range pairs {
		keys[i] = kv.Key
	}
	return keys
}

// TestRenderYAMLMatchesTextOrder is the cross-codec guarantee: a directive
// emitted as YAML lands in the same canonical key order the text codec produces
// after [directive.Reorder], because both call the one Reorder.
func TestRenderYAMLMatchesTextOrder(t *testing.T) {
	t.Parallel()

	providerKeys := []string{"repository"}
	d := directive.Directive{Pairs: []directive.KV{
		{Key: "constraint", Value: "minor"},
		{Key: "find", Value: "schemas/<version>/schema.json"},
		{Key: "tag-prefix", Value: "@biomejs/biome@"},
		{Key: "repository", Value: "biomejs/biome"},
		{Key: "jq", Value: `.["$schema"]`},
		{Key: "provider", Value: "github"},
	}}

	textKeys := pairKeys(directive.Reorder(d, providerKeys).Pairs)
	yamlKeys := mappingKeys(directive.RenderYAML(d, providerKeys))
	require.Equal(t, textKeys, yamlKeys)
	require.Equal(
		t,
		[]string{"provider", "repository", "jq", "find", "constraint", "tag-prefix"},
		yamlKeys,
	)
}

// TestYAMLRoundTrip confirms ParseYAML inverts RenderYAML: the pairs come back
// in canonical order, with a repeatable key expressed as a sequence expanded
// back into repeated pairs.
func TestYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	providerKeys := []string{"repository"}
	d := directive.Directive{Pairs: []directive.KV{
		{Key: "include", Value: "a"},
		{Key: "provider", Value: "github"},
		{Key: "include", Value: "b"},
		{Key: "repository", Value: "biomejs/biome"},
		{Key: "jq", Value: `.["$schema"]`},
		{Key: "tags", Value: "PROD,ci"},
	}}

	node := directive.RenderYAML(d, providerKeys)
	got, err := directive.ParseYAML(node)
	require.NoError(t, err)

	want := directive.CanonicalizeTags(directive.Reorder(d, providerKeys))
	require.Equal(t, want.Pairs, got.Pairs)
	// The repeatable key survived the sequence collapse and re-expansion.
	require.Equal(t, []string{"a", "b"}, got.All("include"))
}

// TestRenderYAMLRepeatableCollapse confirms RenderYAML collapses a repeatable
// key's repeats into one sequence value, while a non-repeatable key that happens
// to repeat is emitted as separate keys (never silently merged into a list).
func TestRenderYAMLRepeatableCollapse(t *testing.T) {
	t.Parallel()

	repeatable := directive.Directive{Pairs: []directive.KV{
		{Key: "include", Value: "a"},
		{Key: "include", Value: "b"},
	}}
	node := directive.RenderYAML(repeatable, nil)
	require.Equal(t, []string{"include"}, mappingKeys(node))
	require.Equal(t, yaml.SequenceNode, node.Content[1].Kind)

	nonRepeatable := directive.Directive{Pairs: []directive.KV{
		{Key: "constraint", Value: "minor"},
		{Key: "constraint", Value: "major"},
	}}
	node = directive.RenderYAML(nonRepeatable, nil)
	require.Equal(t, []string{"constraint", "constraint"}, mappingKeys(node))
	require.Equal(t, yaml.ScalarNode, node.Content[1].Kind)
}

// TestParseYAMLTags confirms a CSV key (tags) accepts both a scalar string and a
// sequence, and that a sequence joins into one comma-separated value - so an item
// that itself holds commas flattens transparently.
func TestParseYAMLTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		yaml      string
		wantValue string   // the single tags value, before canonicalization
		wantCSV   []string // how that value splits for matching
	}{
		{
			name:      "scalar string",
			yaml:      "tags: a,b,c,d",
			wantValue: "a,b,c,d",
			wantCSV:   []string{"a", "b", "c", "d"},
		},
		{
			name:      "flow sequence",
			yaml:      "tags: [a, b, c, d]",
			wantValue: "a,b,c,d",
			wantCSV:   []string{"a", "b", "c", "d"},
		},
		{
			name:      "block sequence with comma item flattens",
			yaml:      "tags:\n  - a\n  - b\n  - c\n  - d,e,f",
			wantValue: "a,b,c,d,e,f",
			wantCSV:   []string{"a", "b", "c", "d", "e", "f"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var node yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &node))

			got, err := directive.ParseYAML(&node)
			require.NoError(t, err)
			value, ok := got.Get("tags")
			require.True(t, ok)
			require.Equal(t, tc.wantValue, value)
			require.Equal(t, tc.wantCSV, got.CSV("tags"))
		})
	}
}

// TestParseYAMLRejectsEmptyValue confirms a directive key never carries an empty
// value: an implicit null, an explicit null, and an empty string are all errors.
func TestParseYAMLRejectsEmptyValue(t *testing.T) {
	t.Parallel()

	for _, y := range []string{"find:", "find: null", `find: ""`} {
		var node yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(y), &node))
		_, err := directive.ParseYAML(&node)
		require.EqualError(t, err, `"find" has an empty value`, "input %q", y)
	}
}

func TestParseYAMLErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "non-mapping node",
			yaml:    "- provider: github\n- repository: a/b",
			wantErr: "sidecar entry must be a YAML mapping",
		},
		{
			name:    "sequence on a non-repeatable key",
			yaml:    "repository:\n  - a\n  - b",
			wantErr: `"repository" does not accept a list value`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var node yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &node))

			_, err := directive.ParseYAML(&node)
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

// TestParseYAMLSequenceForRepeatableKey confirms a sequence value is accepted
// for a repeatable key and expands into one pair per item.
func TestParseYAMLSequenceForRepeatableKey(t *testing.T) {
	t.Parallel()

	var node yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("include:\n  - a\n  - b\n  - c"), &node))

	got, err := directive.ParseYAML(&node)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, got.All("include"))
}
