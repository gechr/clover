package match_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

func TestActionTrackRender(t *testing.T) {
	t.Parallel()

	oldSHA := strings.Repeat("a", 40)
	newSHA := strings.Repeat("b", 40)

	tests := []struct {
		name      string
		line      string
		candidate model.Candidate
		raw       string
		want      string
	}{
		{
			name:      "branch keeps its name, refreshes the commit",
			line:      "  - uses: actions/checkout@" + oldSHA + " # main",
			candidate: model.Candidate{Version: "main", Commit: newSHA},
			raw:       "main",
			want:      "  - uses: actions/checkout@" + newSHA + " # main",
		},
		{
			name:      "explicit ref rewrites the comment too",
			line:      "  - uses: actions/checkout@" + oldSHA + " # main",
			candidate: model.Candidate{Version: "develop", Commit: newSHA},
			raw:       "main",
			want:      "  - uses: actions/checkout@" + newSHA + " # develop",
		},
		{
			name:      "trailing text after the branch is preserved",
			line:      "  - uses: actions/checkout@" + oldSHA + " # main (pinned)",
			candidate: model.Candidate{Version: "main", Commit: newSHA},
			raw:       "main",
			want:      "  - uses: actions/checkout@" + newSHA + " # main (pinned)",
		},
	}

	rw := match.NewActionTrack()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			located, err := rw.Locate(tt.line)
			require.NoError(t, err)
			require.Equal(t, tt.raw, located.Current())
			require.Nil(t, located.Semver())
			require.False(t, located.NeedsDigest())

			out, changed, err := located.Render(tt.line, tt.candidate)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, tt.want, out)
		})
	}
}

func TestActionTrackLocateErrors(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("a", 40)
	tests := []struct {
		name    string
		line    string
		wantErr string
	}{
		{"no uses", "  run: echo hi", "no uses: action reference on the line"},
		{
			"not sha-pinned",
			"  - uses: actions/checkout@v4 # main",
			"action pin requires a full 40-character commit SHA",
		},
		{
			"short sha",
			"  - uses: actions/checkout@" + strings.Repeat("a", 7) + " # main",
			"action pin requires a full 40-character commit SHA",
		},
		{
			"no comment",
			"  - uses: actions/checkout@" + sha,
			"action pin needs a # comment naming the branch",
		},
		{
			"empty comment",
			"  - uses: actions/checkout@" + sha + " #",
			"action pin # comment names no branch",
		},
	}

	rw := match.NewActionTrack()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := rw.Locate(tt.line)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestActionTrackRenderRequiresCommit(t *testing.T) {
	t.Parallel()

	rw := match.NewActionTrack()
	line := "  - uses: actions/checkout@" + strings.Repeat("a", 40) + " # main"
	located, err := rw.Locate(line)
	require.NoError(t, err)

	_, _, err = located.Render(line, model.Candidate{Version: "main"}) // no commit
	require.EqualError(
		t,
		err,
		`candidate has no full commit SHA to pin, got ""`,
		"never half-updates",
	)
}
