package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestRequirementLocate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantErr string
		wantRaw string
	}{
		{
			name:    "floor pin",
			line:    `requires = ["uv_build>=0.8.24"]`,
			wantRaw: "0.8.24",
		},
		{
			name:    "exact pin",
			line:    `  "black==24.1.0",`,
			wantRaw: "24.1.0",
		},
		{
			name:    "compatible-release pin",
			line:    `  "foo~=1.4.2",`,
			wantRaw: "1.4.2",
		},
		{
			name:    "spaced specifier with a dotted name",
			line:    `  "ruamel.yaml >= 0.19.1",`,
			wantRaw: "0.19.1",
		},
		{
			name:    "extras",
			line:    `  "uvicorn[standard]>=0.30.0",`,
			wantRaw: "0.30.0",
		},
		{
			name:    "single-component floor",
			line:    `  "pip>=24",`,
			wantRaw: "24",
		},
		{
			// The environment marker follows the pinned version, so it never
			// disturbs the anchor - not even when it carries a version of its
			// own.
			name:    "environment marker with its own version",
			line:    `  "tomli>=2.0.1 ; python_version < '3.11'",`,
			wantRaw: "2.0.1",
		},
		{
			name:    "no specifier",
			line:    `line-length = 88`,
			wantErr: "no dependency specifier on the line",
		},
		{
			// A single-line group holds several specifiers, so it is ambiguous
			// even when only one pins a version-shaped version.
			name:    "multiple specifiers",
			line:    `dev = ["ruff>=0.15.2", "pytest>=9.0.3"]`,
			wantErr: "multiple dependency specifiers, so it is ambiguous which to track",
		},
		{
			name:    "four-component pin beside a second specifier",
			line:    `deps = ["PyQt5==5.15.2.1", "requests>=2.31"]`,
			wantErr: "multiple dependency specifiers, so it is ambiguous which to track",
		},
		{
			name:    "four-component pin is not version-shaped",
			line:    `  "PyQt5==5.15.2.1",`,
			wantErr: "the specifier pins no version-shaped version",
		},
		{
			name:    "dashless prerelease pin",
			line:    `  "ty>=0.0.0a1",`,
			wantRaw: "0.0.0a1",
		},
		{
			name:    "post-release pin",
			line:    `  "mypkg==1.0.post1",`,
			wantErr: "the constraint continues past the pinned version, so it is ambiguous",
		},
		{
			name:    "wildcard pin",
			line:    `  "foo==1.*",`,
			wantErr: "the constraint continues past the pinned version, so it is ambiguous",
		},
		{
			name:    "range constraint",
			line:    `requires = ["uv_build>=0.11.16,<0.12"]`,
			wantErr: "the constraint continues past the pinned version, so it is ambiguous",
		},
		{
			name:    "local version pin",
			line:    `  "torch==2.6.0+cpu",`,
			wantErr: "a local version pin cannot be re-rendered faithfully",
		},
		{
			// Prose after the version means the string is not a bare
			// specifier, so the tail check rejects it.
			name:    "quoted comparator inside prose",
			line:    `description = "python>=3.8 CLI helper"`,
			wantErr: "the constraint continues past the pinned version, so it is ambiguous",
		},
		{
			name:    "specifier inside a comment",
			line:    `# install "pip>=24" first`,
			wantErr: "no dependency specifier on the line",
		},
		{
			// The version sits inside a nested quote, not at the comparator, so
			// nothing anchors (a uv environments filter, not a dependency).
			name:    "marker expression as the whole value",
			line:    `environments = ["python_version >= '3.10'"]`,
			wantErr: "the specifier pins no version-shaped version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := match.NewRequirement().Locate(tt.line)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantRaw, located.Current())
		})
	}
}

func TestRequirementRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		line        string
		resolved    string
		wantLine    string
		wantChanged bool
	}{
		{
			name:        "floor bump",
			line:        `requires = ["uv_build>=0.8.24"]`,
			resolved:    "0.11.28",
			wantLine:    `requires = ["uv_build>=0.11.28"]`,
			wantChanged: true,
		},
		{
			name:        "style preserved around a spaced specifier",
			line:        `  "ruamel.yaml >= 0.18.0",`,
			resolved:    "0.19.1",
			wantLine:    `  "ruamel.yaml >= 0.19.1",`,
			wantChanged: true,
		},
		{
			// The bump touches the specifier's own version, leaving the
			// marker's untouched.
			name:        "environment marker version stays put",
			line:        `  "tomli>=2.0.1 ; python_version < '3.11'",`,
			resolved:    "2.2.1",
			wantLine:    `  "tomli>=2.2.1 ; python_version < '3.11'",`,
			wantChanged: true,
		},
		{
			name:        "precision preserved on a short floor",
			line:        `  "pip>=24",`,
			resolved:    "26.0.1",
			wantLine:    `  "pip>=26",`,
			wantChanged: true,
		},
		{
			name:        "no-op when already current",
			line:        `requires = ["uv_build>=0.11.28"]`,
			resolved:    "0.11.28",
			wantLine:    `requires = ["uv_build>=0.11.28"]`,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := match.NewRequirement().Locate(tt.line)
			require.NoError(t, err)

			got, changed, err := located.Render(tt.line, model.Candidate{Version: tt.resolved})
			require.NoError(t, err)
			require.Equal(t, tt.wantLine, got)
			require.Equal(t, tt.wantChanged, changed)
		})
	}
}
