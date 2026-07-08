package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestPythonTagLocate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantErr bool
		wantRaw string
	}{
		{name: "py314", line: `target-version = "py314"`, wantRaw: "py314"},
		{name: "py39", line: `target-version = "py39"`, wantRaw: "py39"},
		{name: "py310", line: `target-version = "py310"`, wantRaw: "py310"},
		{name: "no target", line: `target-version = "3.14"`, wantErr: true},
		{name: "array is ambiguous", line: `target-version = ["py311", "py312"]`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := match.NewPythonTag().Locate(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantRaw, located.Current())
		})
	}
}

func TestPythonTagRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		line        string
		resolved    string
		wantLine    string
		wantChanged bool
	}{
		{
			name:        "minor bump",
			line:        `target-version = "py314"`,
			resolved:    "3.15.2",
			wantLine:    `target-version = "py315"`,
			wantChanged: true,
		},
		{
			// The compact form carries only the minor line, so a patch bump within
			// the same minor is a no-op.
			name:     "patch within minor is no-op",
			line:     `target-version = "py314"`,
			resolved: "3.14.6",
			wantLine: `target-version = "py314"`,
		},
		{
			// A double-digit minor renders without a separator: py310, not py3.10.
			name:        "double-digit minor",
			line:        `target-version = "py39"`,
			resolved:    "3.10.0",
			wantLine:    `target-version = "py310"`,
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := match.NewPythonTag().Locate(tt.line)
			require.NoError(t, err)

			candidate := model.NewCandidate(tt.resolved)
			gotLine, changed, err := located.Render(tt.line, candidate)
			require.NoError(t, err)
			require.Equal(t, tt.wantChanged, changed)
			require.Equal(t, tt.wantLine, gotLine)

			// Rendered reports exactly what Render writes into the span.
			renderer, ok := located.(match.Renderer)
			require.True(t, ok)
			require.Equal(t, "py"+renderedMinor(tt.resolved), renderer.Rendered(candidate))
		})
	}
}

// renderedMinor is the major+minor a resolved version renders to, for asserting
// Rendered independently of Render.
func renderedMinor(resolved string) string {
	switch resolved {
	case "3.15.2":
		return "315"
	case "3.14.6":
		return "314"
	case "3.10.0":
		return "310"
	}
	return ""
}
