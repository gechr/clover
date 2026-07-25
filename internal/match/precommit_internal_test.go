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

func TestRevCommitSpan(t *testing.T) {
	t.Parallel()

	const sha = "552baf822992936134cbd31a38f69c8cfe7c0f05"

	tests := []struct {
		name    string
		line    string
		want    string
		wantErr string
	}{
		{
			name: "frozen rev with a version comment",
			line: "    rev: " + sha + "  # frozen: 22.3.0",
			want: sha,
		},
		{
			name: "a quoted scalar",
			line: `    rev: "` + sha + `"  # frozen: v5.0.0`,
			want: sha,
		},
		{
			// A space before the colon is unusual but valid YAML, and the key is
			// still rev.
			name: "space before the colon",
			line: "    rev : " + sha,
			want: sha,
		},
		{
			name:    "a tag rev is not a frozen pin",
			line:    "    rev: v5.0.0",
			wantErr: "rev is not pinned by a full 40-character commit SHA",
		},
		{
			name:    "an abbreviated SHA is not a pin",
			line:    "    rev: 552baf8",
			wantErr: "rev is not pinned by a full 40-character commit SHA",
		},
		{
			name:    "no rev on the line",
			line:    "    repo: https://github.com/psf/black",
			wantErr: "no rev: pin on the line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			span, end, err := revCommitSpan(tt.line)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, tt.line[span.Start:span.End])
			require.Equal(t, span.End, end)
		})
	}
}

// A frozen rev is only inferred where the forge can peel a tag to a commit.
// Elsewhere annotate would propose a marker that every run reports as broken on
// a config the user cannot fix.
func TestInferFrozenRevForgeSupport(t *testing.T) {
	t.Parallel()

	const sha = "552baf822992936134cbd31a38f69c8cfe7c0f05"

	tests := []struct {
		name string
		repo string
		want Inference
	}{
		{
			name: "github peels a tag, so the rev is inferred",
			repo: "https://github.com/psf/black",
			want: Inference{Provider: "github", Repository: "psf/black"},
		},
		{
			name: "gitlab cannot, so it is left for explicit annotation",
			repo: "https://gitlab.com/group/project",
		},
		{
			name: "nor codeberg",
			repo: "https://codeberg.org/owner/tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines := []string{"repos:", "- repo: " + tt.repo, "  rev: " + sha}
			require.Equal(t, tt.want, inferFrozenRev(subject{lines: lines, target: 2}))
		})
	}
}
