package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
)

func TestFirstUnknownKey(t *testing.T) {
	t.Parallel()

	// docker's own keys, supplied by the caller from the resolved provider.
	dockerKeys := []string{"repository", "registry", "platform"}

	tests := []struct {
		name       string
		pairs      []directive.KV
		provider   []string
		wantKey    string
		wantSugger string
		wantFound  bool
	}{
		{
			name: "all common keys known",
			pairs: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "constraint", Value: "minor"},
				{Key: "tags", Value: "ci"},
			},
			wantFound: false,
		},
		{
			name: "provider key known via provider set",
			pairs: []directive.KV{
				{Key: "provider", Value: "docker"},
				{Key: "repository", Value: "redis"},
				{Key: "platform", Value: "linux/amd64"},
			},
			provider:  dockerKeys,
			wantFound: false,
		},
		{
			name: "an unknown stale key is reported without a suggestion",
			pairs: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "max-major", Value: "4"},
			},
			wantKey:   "max-major",
			wantFound: true,
		},
		{
			name: "typo of a common key suggests it",
			pairs: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "constriant", Value: "major"},
			},
			wantKey:    "constriant",
			wantSugger: "constraint",
			wantFound:  true,
		},
		{
			name: "an unknown key near a provider key suggests it",
			pairs: []directive.KV{
				{Key: "provider", Value: "docker"},
				{Key: "repositories", Value: "redis"},
			},
			provider:   dockerKeys,
			wantKey:    "repositories",
			wantSugger: "repository",
			wantFound:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key, suggestion, found := directive.Directive{
				Pairs: tt.pairs,
			}.FirstUnknownKey(
				tt.provider,
			)
			require.Equal(t, tt.wantFound, found)
			require.Equal(t, tt.wantKey, key)
			require.Equal(t, tt.wantSugger, suggestion)
		})
	}
}

// TestCheckKeysSidecarRejectsAnchors pins offset= and target= as inline-only: a
// sidecar entry is located by its own jq/find, so an anchor relative to a
// comment line is meaningless there, while the inline path accepts both as
// common keys.
func TestCheckKeysSidecarRejectsAnchors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key, value, wantErr string
	}{
		{
			key:     "offset",
			value:   "2",
			wantErr: `"offset" is only valid in an inline directive - a sidecar entry locates its line with "find" or "jq"`,
		},
		{
			key:     "target",
			value:   "image:*",
			wantErr: `"target" is only valid in an inline directive - a sidecar entry locates its line with "find" or "jq"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			d := directive.Directive{Pairs: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: tt.key, Value: tt.value},
			}}
			require.NoError(t, d.CheckKeys(nil))
			require.EqualError(t, d.CheckKeysSidecar(nil), tt.wantErr)
		})
	}
}
