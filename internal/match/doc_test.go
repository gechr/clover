package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// TestDocFindReplaceExamples pins every find/replace example in
// docs/find-replace.md to the behavior it documents. Each case is one
// annotation block from the page: the find/replace pair, the input line, and
// the documented Result. The resolved candidate (the version a provider would
// return) is supplied directly so the test stays hermetic. Keep this in lockstep
// with the doc - if an example changes, the test must change with it.
func TestDocFindReplaceExamples(t *testing.T) {
	t.Parallel()

	mk := func(v string) model.Candidate {
		semver, _ := version.Parse(v)
		return model.Candidate{Version: v, Semver: semver}
	}

	tests := []struct {
		doc       string // section of docs/find-replace.md
		find      string
		replace   string
		line      string
		candidate model.Candidate
		want      string
	}{
		// ## find - Glob with placeholders
		{
			doc:  "glob preserves surrounding context",
			find: "toolkit-<version>-linux",
			line: "FROM toolkit-1.2.3-linux AS build", candidate: mk("1.5.0"),
			want: "FROM toolkit-1.5.0-linux AS build",
		},
		{
			doc:  "version styling keeps the v and precision",
			find: "image:<version>",
			line: "image:v1.2", candidate: mk("1.5.0"),
			want: "image:v1.5",
		},
		{
			doc:  "match-only hex is preserved",
			find: "<major.minor.patch>-<hex>.tgz",
			line: "asset: 1.2.3-deadbeef.tgz", candidate: mk("1.5.0"),
			want: "asset: 1.5.0-deadbeef.tgz",
		},
		// ## find - Regular expressions
		{
			doc:  "regex in place substitutes the captured version",
			find: `/v(\d+\.\d+\.\d+)/`,
			line: "tag = v1.2.3", candidate: mk("1.5.0"),
			want: "tag = v1.5.0",
		},
		// ## replace - rendering the result
		{
			doc:  "compose a short series and the full version",
			find: "<version>", replace: "<major.minor> (<version>)",
			line: "release = 1.2.3", candidate: mk("1.5.0"),
			want: "release = 1.5 (1.5.0)",
		},
		{
			doc:  "echo a matched build hash while reformatting",
			find: "<major.minor.patch>+build.<hex>", replace: "v<major.minor.patch> (<hex>)",
			line: "ver = 1.2.3+build.deadbeef", candidate: mk("1.5.0"),
			want: "ver = v1.5.0 (deadbeef)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.doc, func(t *testing.T) {
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
