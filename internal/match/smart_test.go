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
			require.Equal(t, tt.wantRaw, located.Raw)
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

			smart := match.NewSmart()
			located, err := smart.Locate(tt.line)
			require.NoError(t, err)

			got, changed, err := smart.Render(
				tt.line,
				located,
				model.Candidate{Version: tt.resolved},
			)
			require.NoError(t, err)
			require.Equal(t, tt.wantLine, got)
			require.Equal(t, tt.wantChanged, changed)
		})
	}
}
