package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestSmartLocate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		wantErr bool
		wantRaw string
	}{
		{name: "single token", line: "FROM nginx:1.27.0", wantRaw: "1.27.0"},
		{name: "v-prefixed token", line: "tag: v1.2.3", wantRaw: "v1.2.3"},
		{name: "no token", line: "FROM nginx:latest", wantErr: true},
		{name: "ambiguous", line: "from 1.2.3 to 1.3.0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := match.NewSmart().Locate(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantRaw, located.Current())
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
			// A prerelease is only ever selected deliberately (prerelease=true),
			// so it must be rendered even onto a clean line; trimming it would
			// write a stable version that does not exist yet.
			name:        "prerelease kept when current clean",
			line:        "app:1.2.3",
			resolved:    "1.3.0-rc.1",
			wantLine:    "app:1.3.0-rc.1",
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
			// The go provider's canonical dashless prerelease: a clean 3-part Go
			// line with prerelease=true must render the rc, not a fake 1.27.0.
			name:        "go prerelease onto clean toolchain line",
			line:        "GO_VERSION=1.26.5",
			resolved:    "1.27.0-rc2",
			wantLine:    "GO_VERSION=1.27.0-rc2",
			wantChanged: true,
		},
		{
			// A dashless PEP 440 pin keeps its spelling: the resolved canonical
			// prerelease is re-glued without the dash, so the value stays valid
			// for the Python tools reading the line.
			name:        "dashless prerelease spelling preserved",
			line:        `python = "3.15.0b3"`,
			resolved:    "3.15.0-rc1",
			wantLine:    `python = "3.15.0rc1"`,
			wantChanged: true,
		},
		{
			name:        "dashless prerelease to stable",
			line:        "PYTHON_VERSION: 3.15.0b3",
			resolved:    "3.15.1",
			wantLine:    "PYTHON_VERSION: 3.15.1",
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

			located, err := match.NewSmart().Locate(tt.line)
			require.NoError(t, err)

			got, changed, err := located.Render(tt.line, model.Candidate{Version: tt.resolved})
			require.NoError(t, err)
			require.Equal(t, tt.wantLine, got)
			require.Equal(t, tt.wantChanged, changed)
		})
	}
}
