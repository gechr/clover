package match_test

import (
	"testing"

	"github.com/gechr/cusp/internal/match"
	"github.com/stretchr/testify/require"
)

func TestSmartLocate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		line     string
		wantOK   bool
		wantCore string
	}{
		{name: "single token", line: "FROM nginx:1.27.0", wantOK: true, wantCore: "1.27.0"},
		{name: "no token", line: "FROM nginx:latest", wantOK: false},
		{name: "ambiguous", line: "from 1.2.3 to 1.3.0", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tok, ok := match.NewSmart().Locate(tt.line)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.wantCore, tok.Core)
			}
		})
	}
}

func TestSmartRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		line        string
		resolved    string
		wantLine    string
		wantChanged bool
	}{
		{
			name: "three part bump", line: "FROM nginx:1.27.0", resolved: "1.28.0",
			wantLine: "FROM nginx:1.28.0", wantChanged: true,
		},
		{
			name: "precision truncates to current", line: "redis:7.2", resolved: "7.4.1",
			wantLine: "redis:7.4", wantChanged: true,
		},
		{
			name: "precision pads to current", line: "redis:7.2.0", resolved: "7.4",
			wantLine: "redis:7.4.0", wantChanged: true,
		},
		{
			name: "v prefix preserved", line: "tag: v1.2.3", resolved: "1.3.0",
			wantLine: "tag: v1.3.0", wantChanged: true,
		},
		{
			name: "bare drops candidate prefix", line: "tag: 1.2.3", resolved: "v1.3.0",
			wantLine: "tag: 1.3.0", wantChanged: true,
		},
		{
			name:        "variant suffix preserved",
			line:        "FROM nginx:1.27-alpine",
			resolved:    "1.28-alpine",
			wantLine:    "FROM nginx:1.28-alpine",
			wantChanged: true,
		},
		{
			name:        "variant re-applied from bare candidate",
			line:        "FROM nginx:1.27-alpine",
			resolved:    "1.28",
			wantLine:    "FROM nginx:1.28-alpine",
			wantChanged: true,
		},
		{
			name:        "prerelease trimmed when current clean",
			line:        "app:1.2.3",
			resolved:    "1.3.0-rc.1",
			wantLine:    "app:1.3.0",
			wantChanged: true,
		},
		{
			name:        "prerelease kept when current has one",
			line:        "app:1.2.3-rc.1",
			resolved:    "1.3.0-rc.2",
			wantLine:    "app:1.3.0-rc.2",
			wantChanged: true,
		},
		{
			name:        "trailing content preserved",
			line:        "FROM nginx:1.27-alpine # pinned",
			resolved:    "1.28-alpine",
			wantLine:    "FROM nginx:1.28-alpine # pinned",
			wantChanged: true,
		},
		{
			name: "idempotent when unchanged", line: "FROM nginx:1.27.0", resolved: "1.27.0",
			wantLine: "FROM nginx:1.27.0", wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			current, ok := match.NewSmart().Locate(tt.line)
			require.True(t, ok)

			got, changed := match.NewSmart().Render(tt.line, current, tt.resolved)
			require.Equal(t, tt.wantLine, got)
			require.Equal(t, tt.wantChanged, changed)
		})
	}
}
