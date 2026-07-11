package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRenderYAMLList(t *testing.T) {
	t.Parallel()

	entry := func(pairs ...directive.KV) *yaml.Node {
		return directive.RenderYAML(directive.Directive{Pairs: pairs}, []string{"repository"})
	}

	tests := map[string]struct {
		entries []*yaml.Node
		want    string
	}{
		"nil entries": {
			entries: nil,
			want:    "[]\n",
		},
		"one entry": {
			entries: []*yaml.Node{entry(
				directive.KV{Key: "provider", Value: "github"},
				directive.KV{Key: "repository", Value: "cli/cli"},
			)},
			want: "- provider: github\n  repository: cli/cli\n",
		},
		"two entries": {
			entries: []*yaml.Node{
				entry(
					directive.KV{Key: "provider", Value: "github"},
					directive.KV{Key: "repository", Value: "cli/cli"},
				),
				entry(
					directive.KV{Key: "provider", Value: "docker"},
					directive.KV{Key: "repository", Value: "redis"},
				),
			},
			want: "- provider: github\n  repository: cli/cli\n- provider: docker\n  repository: redis\n",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := directive.RenderYAMLList(tc.entries)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
		})
	}
}
