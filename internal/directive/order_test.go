package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	xslices "github.com/gechr/x/slices"
	"github.com/stretchr/testify/require"
)

func TestReorder(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		providerKeys []string
		want         []string // expected key order after reordering
	}{
		{
			name:         "canonical zones",
			in:           "clover: disabled=false repository=a/b provider=github constraint=patch",
			providerKeys: []string{"repository", "source"},
			want:         []string{"provider", "repository", "constraint", "disabled"},
		},
		{
			name:         "provider keys in declared order",
			in:           "clover: source=tags provider=github repository=a/b",
			providerKeys: []string{"repository", "source"},
			want:         []string{"provider", "repository", "source"},
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
			name:         "tags sort before disabled in the trailing zone",
			in:           "clover: disabled=false tags=prod provider=github repository=a/b",
			providerKeys: []string{"repository"},
			want:         []string{"provider", "repository", "tags", "disabled"},
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
			keys := xslices.Map(got.Pairs, func(kv directive.KV) string { return kv.Key })
			require.Equal(t, tc.want, keys)
		})
	}
}

// TestReorderLocatorZone confirms the locator keys sort after the provider's own
// keys and before the trailing selection rules, so an entry reads
// source -> locator -> selection.
func TestReorderLocatorZone(t *testing.T) {
	d, found, err := directive.Parse(
		"clover: constraint=minor find=x replace=y jq=.v repository=a/b provider=github",
	)
	require.NoError(t, err)
	require.True(t, found)

	got := directive.Reorder(d, []string{"repository"})
	keys := xslices.Map(got.Pairs, func(kv directive.KV) string { return kv.Key })
	require.Equal(
		t,
		[]string{"provider", "repository", "jq", "find", "replace", "constraint"},
		keys,
	)
}

// TestReorderPreservesValues confirms reordering moves pairs without altering
// their values or dropping any.
func TestReorderPreservesValues(t *testing.T) {
	d, _, err := directive.Parse(
		"clover: disabled=true provider=github repository=a/b include=x include=y",
	)
	require.NoError(t, err)

	got := directive.Reorder(d, []string{"repository"})
	require.Len(t, got.Pairs, 5)
	require.ElementsMatch(t, d.Pairs, got.Pairs)
}

// TestReorderIdempotent confirms a second reorder changes nothing.
func TestReorderIdempotent(t *testing.T) {
	d, _, err := directive.Parse("clover: disabled=false repository=a/b provider=github")
	require.NoError(t, err)

	once := directive.Reorder(d, []string{"repository"})
	twice := directive.Reorder(once, []string{"repository"})
	require.Equal(t, once.Pairs, twice.Pairs)
}
