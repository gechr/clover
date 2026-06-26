package match_test

import (
	"strings"
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

func cand(t *testing.T, v, commit string) model.Candidate {
	t.Helper()
	semver, _ := version.Parse(v)
	return model.Candidate{Version: v, Semver: semver, Commit: commit}
}

func TestFindReplaceRender(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("a", 40)
	tests := []struct {
		name      string
		find      string
		replace   string
		line      string
		candidate model.Candidate
		want      string
	}{
		{
			name: "glob in place preserves context",
			find: "catalyst-<version>-linux", line: "FROM catalyst-1.2.3-linux AS build",
			candidate: cand(t, "1.5.0", ""),
			want:      "FROM catalyst-1.5.0-linux AS build",
		},
		{
			name: "glob preserves a match-only hex",
			find: "<major.minor.patch>-<hex>.tgz", line: "asset: 1.2.3-deadbeef.tgz",
			candidate: cand(t, "1.5.0", ""),
			want:      "asset: 1.5.0-deadbeef.tgz",
		},
		{
			name: "glob version styling keeps the v and precision",
			find: "image:<version>", line: "image:v1.2",
			candidate: cand(t, "1.5.0", ""),
			want:      "image:v1.5",
		},
		{
			name: "replace template reorders and echoes the matched hex",
			find: "<version>_<hex>", replace: "<hex>HELLO<version>", line: "pkg 1.2.3_deadbeef end",
			candidate: cand(t, "1.5.0", ""),
			want:      "pkg deadbeefHELLO1.5.0 end",
		},
		{
			name: "regex action pin updates commit and comment",
			find: `/@\S+\s*#\s*(\S+)/`, replace: "@<commit> # <version>",
			line:      "  uses: actions/checkout@" + sha + " # v4",
			candidate: cand(t, "5.0.0", strings.Repeat("b", 40)),
			want:      "  uses: actions/checkout@" + strings.Repeat("b", 40) + " # v5",
		},
		{
			name: "regex in place substitutes the captured version",
			find: `/v(\d+\.\d+\.\d+)/`, line: "tag = v1.2.3",
			candidate: cand(t, "1.5.0", ""),
			want:      "tag = v1.5.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fr, err := match.NewFindReplace(tt.find, tt.replace)
			require.NoError(t, err)
			located, err := fr.Locate(tt.line)
			require.NoError(t, err)
			out, changed, err := located.Render(tt.line, tt.candidate)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, tt.want, out)
		})
	}
}

func TestFindReplaceLocateNoMatch(t *testing.T) {
	t.Parallel()

	fr, err := match.NewFindReplace("catalyst-<version>", "")
	require.NoError(t, err)
	_, err = fr.Locate("nothing here")
	require.EqualError(t, err, "find pattern did not match the target line")
}

func TestFindReplaceUnavailableToken(t *testing.T) {
	t.Parallel()

	fr, err := match.NewFindReplace("<version>", "<version>-<sha256>")
	require.NoError(t, err)
	located, err := fr.Locate("v1.2.3")
	require.NoError(t, err)
	_, _, err = located.Render("v1.2.3", cand(t, "1.5.0", "")) // no digest
	require.EqualError(t, err, `replace "<version>-<sha256>" references an unavailable token`)
}
