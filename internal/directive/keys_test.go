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
