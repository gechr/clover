package follow_test

import (
	"testing"

	"github.com/gechr/cusp/internal/model"
	"github.com/gechr/cusp/internal/provider/follow"
	"github.com/gechr/cusp/internal/registry"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	reg.Set("tool", model.Candidate{Version: "1.4.0", Commit: "abc123"})

	tests := []struct {
		name    string
		from    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "version", from: "tool", value: "version", want: "1.4.0"},
		{name: "empty defaults to version", from: "tool", value: "", want: "1.4.0"},
		{name: "commit", from: "tool", value: "commit", want: "abc123"},
		{name: "unknown producer", from: "missing", value: "version", wantErr: true},
		{name: "unknown value", from: "tool", value: "digest", wantErr: true},
		{name: "sha256 not yet", from: "tool", value: "sha256", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := follow.Resolve(reg, tt.from, tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveCommitMissing(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	reg.Set("tool", model.Candidate{Version: "1.4.0"}) // no commit

	_, err := follow.Resolve(reg, "tool", "commit")
	require.Error(t, err)
}
