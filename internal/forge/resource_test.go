package forge_test

import (
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/forge"
	"github.com/stretchr/testify/require"
)

// dir builds a directive from alternating key/value arguments.
func dir(pairs ...string) directive.Directive {
	var d directive.Directive
	for i := 0; i+1 < len(pairs); i += 2 {
		d.Pairs = append(d.Pairs, directive.KV{Key: pairs[i], Value: pairs[i+1]})
	}
	return d
}

func TestOwnerName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		d         directive.Directive
		wantOwner string
		wantName  string
		wantErr   string
	}{
		"missing key": {
			d:       dir(),
			wantErr: `gitea: "repository" is required`,
		},
		"owner and name": {
			d:         dir(constant.DirectiveRepository, "o/n"),
			wantOwner: "o",
			wantName:  "n",
		},
		"no slash": {
			d:       dir(constant.DirectiveRepository, "o"),
			wantErr: `gitea: "repository" must be owner/name, got "o"`,
		},
		"empty name": {
			d:       dir(constant.DirectiveRepository, "o/"),
			wantErr: `gitea: "repository" must be owner/name, got "o/"`,
		},
		"empty owner": {
			d:       dir(constant.DirectiveRepository, "/n"),
			wantErr: `gitea: "repository" must be owner/name, got "/n"`,
		},
		"three segments": {
			d:       dir(constant.DirectiveRepository, "a/b/c"),
			wantErr: `gitea: "repository" must be owner/name, got "a/b/c"`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			owner, repo, err := forge.OwnerName("gitea", tt.d)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				require.Empty(t, owner)
				require.Empty(t, repo)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantOwner, owner)
			require.Equal(t, tt.wantName, repo)
		})
	}
}

func TestHost(t *testing.T) {
	t.Parallel()

	const def = "codeberg.org"

	tests := map[string]struct {
		d       directive.Directive
		want    string
		wantErr string
	}{
		"absent uses default": {
			d:    dir(),
			want: def,
		},
		"present normalized": {
			d:    dir(constant.DirectiveHost, "Git.Example.com"),
			want: "git.example.com",
		},
		"invalid host": {
			d:       dir(constant.DirectiveHost, "git.example.com/path"),
			wantErr: `gitea: "host" must be a valid host, got "git.example.com/path"`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			host, err := forge.Host("gitea", tt.d, def)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				require.Empty(t, host)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, host)
		})
	}
}

func TestSource(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		d       directive.Directive
		want    string
		wantErr string
	}{
		"absent defaults to tags": {
			d:    dir(),
			want: forge.SourceTags,
		},
		"explicit tags": {
			d:    dir(forge.KeySource, forge.SourceTags),
			want: forge.SourceTags,
		},
		"explicit releases": {
			d:    dir(forge.KeySource, forge.SourceReleases),
			want: forge.SourceReleases,
		},
		"invalid source": {
			d:       dir(forge.KeySource, "commits"),
			wantErr: `gitea: "source" must be tags or releases, got "commits"`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source, err := forge.Source("gitea", tt.d)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				require.Empty(t, source)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, source)
		})
	}
}

func TestRequireReleasesForAsset(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		d       directive.Directive
		source  string
		wantErr string
	}{
		"asset with tags": {
			d:       dir(constant.RuleAsset, "clover_*"),
			source:  forge.SourceTags,
			wantErr: `gitea: "asset" requires "source" to be "releases"`,
		},
		"asset with releases": {
			d:      dir(constant.RuleAsset, "clover_*"),
			source: forge.SourceReleases,
		},
		"no asset": {
			d:      dir(),
			source: forge.SourceTags,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := forge.RequireReleasesForAsset("gitea", tt.d, tt.source)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
