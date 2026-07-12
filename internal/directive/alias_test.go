package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
)

func TestCanonicalizeAliases(t *testing.T) {
	t.Parallel()

	kv := func(pairs ...directive.KV) directive.Directive {
		return directive.Directive{Pairs: pairs}
	}

	tests := []struct {
		name string
		in   directive.Directive
		want directive.Directive
	}{
		{
			name: "key alias rewrites flavour to flavor, value preserved",
			in:   kv(directive.KV{Key: "flavour", Value: "codeberg"}),
			want: kv(directive.KV{Key: "flavor", Value: "codeberg"}),
		},
		{
			name: "value alias rewrites golang to go under provider",
			in:   kv(directive.KV{Key: "provider", Value: "golang"}),
			want: kv(directive.KV{Key: "provider", Value: "go"}),
		},
		{
			name: "value alias is key-scoped: golang under another key is untouched",
			in:   kv(directive.KV{Key: "include", Value: "golang"}),
			want: kv(directive.KV{Key: "include", Value: "golang"}),
		},
		{
			name: "canonical spellings are left verbatim",
			in: kv(
				directive.KV{Key: "provider", Value: "go"},
				directive.KV{Key: "flavor", Value: "gitea"},
			),
			want: kv(
				directive.KV{Key: "provider", Value: "go"},
				directive.KV{Key: "flavor", Value: "gitea"},
			),
		},
		{
			name: "both aliases rewrite together, order preserved",
			in: kv(
				directive.KV{Key: "provider", Value: "golang"},
				directive.KV{Key: "track", Value: "*"},
				directive.KV{Key: "flavour", Value: "forgejo"},
			),
			want: kv(
				directive.KV{Key: "provider", Value: "go"},
				directive.KV{Key: "track", Value: "*"},
				directive.KV{Key: "flavor", Value: "forgejo"},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, directive.CanonicalizeAliases(tt.in))
		})
	}
}
