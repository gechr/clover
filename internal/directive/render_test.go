package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	tests := []struct {
		name string
		in   string // a directive body to parse, then render
		want string
	}{
		{"single pair", "clover: provider=github", "clover: provider=github"},
		{"two pairs", "clover: provider=github repo=a/b", "clover: provider=github repo=a/b"},
		{"slash in bare value", "clover: repo=owner/name", "clover: repo=owner/name"},
		{
			"collapses extra spaces",
			"clover:   provider=github    repo=a/b",
			"clover: provider=github repo=a/b",
		},
		{"quoted value with space re-quoted", `clover: include="a b"`, `clover: include="a b"`},
		{"redundant quotes dropped", `clover: repo="owner/name"`, "clover: repo=owner/name"},
		{"complete regex stays bare", "clover: include=/foo.*/", "clover: include=/foo.*/"},
		{"regex with space stays bare", "clover: include=/foo bar/", "clover: include=/foo bar/"},
		{"escaped slash regex", `clover: include=/a\/b/`, `clover: include=/a\/b/`},
		{"quoted partial-slash re-quoted", `clover: include="/foo"`, `clover: include="/foo"`},
		{"boolean explicit", "clover: skip=true", "clover: skip=true"},
		{"repeated keys preserved", "clover: include=a include=b", "clover: include=a include=b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, found, err := directive.Parse(tc.in)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, tc.want, directive.Render(d))
		})
	}
}

// TestRenderQuotesSpecialLeadingChars covers values that must be quoted so the
// parser does not mistake their leading character for a delimiter.
func TestRenderQuotesSpecialLeadingChars(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"leading double quote", "k", `"x`, "clover: k='\"x'"},
		{"leading single quote", "k", "'x", "clover: k=\"'x\""},
		{"value with space", "k", "a b", `clover: k="a b"`},
		{"plain value bare", "k", "abc", "clover: k=abc"},
		{"empty value bare", "k", "", "clover: k="},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := directive.Directive{Pairs: []directive.KV{{Key: tc.key, Value: tc.value}}}
			require.Equal(t, tc.want, directive.Render(d))
		})
	}
}

// TestRenderParseRoundTrip is the core property: rendering a parsed directive
// and parsing it again yields the identical pairs, so format is lossless.
func TestRenderParseRoundTrip(t *testing.T) {
	bodies := []string{
		"clover: provider=github repo=owner/name source=releases",
		`clover: include="a b" exclude=/x.*/`,
		"clover: from=app value=commit select=old",
		`clover: constraint=">=1.2,<2.0"`,
		`clover: include="/foo"`,
		"clover: include=/a\\/b/ skip=false",
	}

	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			first, found, err := directive.Parse(body)
			require.NoError(t, err)
			require.True(t, found)

			rendered := directive.Render(first)
			second, found, err := directive.Parse(rendered)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(
				t,
				first.Pairs,
				second.Pairs,
				"round-trip changed pairs (rendered: %q)",
				rendered,
			)

			// Idempotent: rendering the re-parsed directive is byte-identical.
			require.Equal(t, rendered, directive.Render(second))
		})
	}
}

func TestReorder(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		providerKeys []string
		want         []string // expected key order after reordering
	}{
		{
			name:         "canonical zones",
			in:           "clover: skip=false repo=a/b provider=github constraint=patch",
			providerKeys: []string{"repo", "source"},
			want:         []string{"provider", "repo", "constraint", "skip"},
		},
		{
			name:         "provider keys in declared order",
			in:           "clover: source=tags provider=github repo=a/b",
			providerKeys: []string{"repo", "source"},
			want:         []string{"provider", "repo", "source"},
		},
		{
			name:         "follow keys lead",
			in:           "clover: value=commit from=app select=new",
			providerKeys: nil,
			want:         []string{"from", "value", "select"},
		},
		{
			name:         "rule keys ordered",
			in:           "clover: behind=1 include=a constraint=minor prerelease=true",
			providerKeys: nil,
			want:         []string{"constraint", "include", "prerelease", "behind"},
		},
		{
			name:         "tags sort before skip in the trailing zone",
			in:           "clover: skip=false tags=prod provider=github repo=a/b",
			providerKeys: []string{"repo"},
			want:         []string{"provider", "repo", "tags", "skip"},
		},
		{
			name:         "unknown keys kept last in original order",
			in:           "clover: zeta=1 provider=github alpha=2",
			providerKeys: nil,
			want:         []string{"provider", "zeta", "alpha"},
		},
		{
			name:         "repeated keys keep relative order",
			in:           "clover: include=a exclude=x include=b",
			providerKeys: nil,
			want:         []string{"include", "include", "exclude"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, found, err := directive.Parse(tc.in)
			require.NoError(t, err)
			require.True(t, found)

			got := directive.Reorder(d, tc.providerKeys)
			keys := make([]string, len(got.Pairs))
			for i, kv := range got.Pairs {
				keys[i] = kv.Key
			}
			require.Equal(t, tc.want, keys)
		})
	}
}

// TestReorderPreservesValues confirms reordering moves pairs without altering
// their values or dropping any.
func TestReorderPreservesValues(t *testing.T) {
	d, _, err := directive.Parse("clover: skip=true provider=github repo=a/b include=x include=y")
	require.NoError(t, err)

	got := directive.Reorder(d, []string{"repo"})
	require.Len(t, got.Pairs, 5)
	require.ElementsMatch(t, d.Pairs, got.Pairs)
}

// TestReorderIdempotent confirms a second reorder changes nothing.
func TestReorderIdempotent(t *testing.T) {
	d, _, err := directive.Parse("clover: skip=false repo=a/b provider=github")
	require.NoError(t, err)

	once := directive.Reorder(d, []string{"repo"})
	twice := directive.Reorder(once, []string{"repo"})
	require.Equal(t, once.Pairs, twice.Pairs)
}
