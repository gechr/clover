package docker_test

import (
	"testing"

	"github.com/gechr/clover/internal/provider/docker"
	"github.com/stretchr/testify/require"
)

func TestParseChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		wantRealm string
		wantScope string
	}{
		{
			name:      "realm service and scope",
			header:    `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`,
			wantRealm: "https://auth.docker.io/token",
			wantScope: "repository:library/nginx:pull",
		},
		{
			name:      "scope with an embedded comma stays intact",
			header:    `Bearer realm="https://ghcr.io/token",scope="repository:owner/img:pull,push"`,
			wantRealm: "https://ghcr.io/token",
			wantScope: "repository:owner/img:pull,push",
		},
		{
			name:   "a non-bearer scheme yields no realm",
			header: `Basic realm="https://example.com"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			realm, params := docker.ParseChallenge(tt.header)
			require.Equal(t, tt.wantRealm, realm)
			require.Equal(t, tt.wantScope, params["scope"])
		})
	}
}
