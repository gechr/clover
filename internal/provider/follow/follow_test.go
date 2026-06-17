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
	reg.Set("tool", registry.Entry{
		Old: model.Candidate{Version: "1.3.0", Commit: "old111"},
		New: model.Candidate{Version: "1.4.0", Commit: "abc123"},
	})

	tests := []struct {
		name    string
		from    string
		value   string
		when    string
		want    string
		wantErr bool
	}{
		{name: "version defaults to new", from: "tool", value: "version", want: "1.4.0"},
		{name: "empty value defaults to version", from: "tool", value: "", want: "1.4.0"},
		{name: "commit new", from: "tool", value: "commit", want: "abc123"},
		{name: "version old", from: "tool", value: "version", when: "old", want: "1.3.0"},
		{name: "commit old", from: "tool", value: "commit", when: "old", want: "old111"},
		{name: "explicit new", from: "tool", value: "version", when: "new", want: "1.4.0"},
		{name: "unknown producer", from: "missing", value: "version", wantErr: true},
		{name: "unknown value", from: "tool", value: "digest", wantErr: true},
		{name: "unknown selector", from: "tool", value: "version", when: "previous", wantErr: true},
		{name: "sha256 not yet", from: "tool", value: "sha256", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := follow.Resolve(reg, tt.from, tt.value, tt.when)
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
	reg.Set("tool", registry.Entry{New: model.Candidate{Version: "1.4.0"}}) // no commit

	_, err := follow.Resolve(reg, "tool", "commit", "")
	require.Error(t, err)
}
