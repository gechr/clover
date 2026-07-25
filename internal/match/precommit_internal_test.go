package match

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForgeReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		host       string
		repository string
		ok         bool
	}{
		{
			name:       "https clone url",
			raw:        "https://github.com/owner/name",
			host:       "github.com",
			repository: "owner/name",
			ok:         true,
		},
		{
			name:       "the .git suffix reaches the same repository",
			raw:        "https://github.com/owner/name.git",
			host:       "github.com",
			repository: "owner/name",
			ok:         true,
		},
		{
			name:       "a trailing slash is not part of the name",
			raw:        "https://github.com/owner/name/",
			host:       "github.com",
			repository: "owner/name",
			ok:         true,
		},
		{
			name:       "scp-like ssh remote",
			raw:        "git@github.com:owner/name.git",
			host:       "github.com",
			repository: "owner/name",
			ok:         true,
		},
		{
			name:       "ssh scheme with a user",
			raw:        "ssh://git@codeberg.org/owner/name",
			host:       "codeberg.org",
			repository: "owner/name",
			ok:         true,
		},
		{
			name:       "a nested project keeps every segment",
			raw:        "https://gitlab.com/group/subgroup/project",
			host:       "gitlab.com",
			repository: "group/subgroup/project",
			ok:         true,
		},
		{
			name:       "a hostname is case-insensitive",
			raw:        "https://GitHub.com/Owner/Name",
			host:       "GitHub.com",
			repository: "Owner/Name",
			ok:         true,
		},
		{
			name:       "a port is not read as the first path segment",
			raw:        "https://git.example.com:8443/owner/name",
			host:       "git.example.com",
			repository: "owner/name",
			ok:         true,
		},
		{
			// pre-commit's pseudo-repositories for repository-local and meta hooks.
			name: "local is no reference",
			raw:  "local",
		},
		{
			name: "meta is no reference",
			raw:  "meta",
		},
		{
			name: "empty is no reference",
			raw:  "",
		},
		{
			name: "a host alone names no repository",
			raw:  "https://github.com",
		},
		{
			name: "a single path segment is not owner/name",
			raw:  "https://github.com/owner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			host, repository, ok := forgeReference(tt.raw)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.host, host)
			require.Equal(t, tt.repository, repository)
		})
	}
}
