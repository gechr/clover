package pipeline

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// digesterStub adds the Digester capability to filterStub; committerStub adds
// Committer. filterStub itself implements neither, so it drives the incapable
// branches.
type digesterStub struct{ filterStub }

func (digesterStub) Digest(context.Context, provider.Resource, string) (string, error) {
	return "", nil
}

type committerStub struct{ filterStub }

func (committerStub) Commit(context.Context, provider.Resource, string) (string, error) {
	return "", nil
}

func TestTrackCapable(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		prov        provider.Provider
		needsDigest bool
		wantErr     string
	}{
		"digest needed, provider digests": {
			prov: digesterStub{filterStub{name: "d"}}, needsDigest: true,
		},
		"digest needed, provider cannot": {
			prov: filterStub{name: "d"}, needsDigest: true,
			wantErr: `provider "d" cannot resolve a digest for a tracked tag`,
		},
		"commit needed, provider commits": {
			prov: committerStub{filterStub{name: "c"}}, needsDigest: false,
		},
		"commit needed, provider cannot": {
			prov: filterStub{name: "c"}, needsDigest: false,
			wantErr: `provider "c" cannot resolve a commit for a tracked branch`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := trackCapable(tt.prov, tt.prov.Name(), tt.needsDigest)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}
