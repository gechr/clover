package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

const (
	oldSHA = "1234567890abcdef1234567890abcdef12345678"
	newSHA = "abcdef1234567890abcdef1234567890abcdef12"
)

func TestActionPinRoundTrip(t *testing.T) {
	t.Parallel()

	line := "  - uses: actions/checkout@" + oldSHA + " # v4.1.0"

	located, err := match.NewActionPin().Locate(line)
	require.NoError(t, err)
	require.Equal(t, "v4.1.0", located.Current())

	got, changed, err := located.Render(line, model.Candidate{
		Version: "4.2.0",
		Commit:  newSHA,
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "  - uses: actions/checkout@"+newSHA+" # v4.2.0", got)
}

func TestActionPinSubdirAndQuotes(t *testing.T) {
	t.Parallel()

	// A subpath action still pins on the SHA after @, and the comment anchors it.
	line := `  - uses: "owner/repo/path@` + oldSHA + `" # v1.0.0`

	located, err := match.NewActionPin().Locate(line)
	require.NoError(t, err)

	got, _, err := located.Render(line, model.Candidate{Version: "1.1.0", Commit: newSHA})
	require.NoError(t, err)
	require.Equal(t, `  - uses: "owner/repo/path@`+newSHA+`" # v1.1.0`, got)
}

func TestActionPinRendered(t *testing.T) {
	t.Parallel()

	// The comment is # v3.5.0 (v-prefixed, 3-part); an upstream tag with a bare
	// core (e.g. v7 -> "7") is restyled to match, and Rendered reports that
	// written text so the run report shows v7.0.0, not the raw candidate "7".
	line := "  - uses: actions/checkout@" + oldSHA + " # v3.5.0"
	located, err := match.NewActionPin().Locate(line)
	require.NoError(t, err)

	r, ok := located.(match.Renderer)
	require.True(t, ok, "action pin must report its rendered value")
	require.Equal(t, "v7.0.0", r.Rendered(model.Candidate{Version: "7", Commit: newSHA}))
}

func TestActionPinLocateErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"no uses":        "FROM nginx:1.27",
		"local action":   "  - uses: ./.github/actions/build",
		"docker action":  "  - uses: docker://alpine:3.18",
		"tag pinned":     "  - uses: actions/checkout@v4",
		"short sha":      "  - uses: actions/checkout@abc123 # v4",
		"no comment":     "  - uses: actions/checkout@" + oldSHA,
		"comment no ver": "  - uses: actions/checkout@" + oldSHA + " # pinned",
	}

	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := match.NewActionPin().Locate(line)
			require.Error(t, err)
		})
	}
}

func TestActionPinRenderRequiresFullCommit(t *testing.T) {
	t.Parallel()

	line := "  - uses: actions/checkout@" + oldSHA + " # v4.1.0"
	located, err := match.NewActionPin().Locate(line)
	require.NoError(t, err)

	for _, bad := range []string{"", "abc123", "not-hex-not-hex-not-hex-not-hex-not-hex!"} {
		_, _, err := located.Render(line, model.Candidate{Version: "4.2.0", Commit: bad})
		require.Error(t, err, "commit %q must be rejected", bad)
	}
}

func TestForRoutesSHAPinToActionPin(t *testing.T) {
	t.Parallel()

	pinLine := "  - uses: actions/checkout@" + oldSHA + " # v4.1.0"
	// The secure-pin shape selects the action-pin rewriter regardless of where
	// the file lives - a workflow, a composite action.yml, or anywhere else a
	// SHA-pinned reference appears - so the SHA and comment never desync.
	for _, path := range []string{
		".github/workflows/ci.yml",
		"/abs/repo/.github/workflows/ci.yml",
		"sub/dir/.github/workflows/release.yaml",
		".github/actions/setup/action.yml",
		"deeply/nested/composite/action.yaml",
		"hello.github/workflows/x.yml", // not under a real .github segment, still a pin
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			rw := match.For(match.Context{Path: path, Line: pinLine, Provider: "github"})
			require.IsType(t, match.ActionPin{}, rw)
		})
	}
}

func TestForLeavesNonPinToSmart(t *testing.T) {
	t.Parallel()

	pinLine := "  - uses: actions/checkout@" + oldSHA + " # v4.1.0"
	tests := map[string]match.Context{
		// No uses: pin at all.
		"dockerfile": {Path: "Dockerfile", Line: "FROM nginx:1.27.0", Provider: "github"},
		// A tag-pinned reference carries no paired SHA, so smart bumps the ref.
		"tag pinned": {
			Path:     ".github/workflows/ci.yml",
			Line:     "  - uses: actions/checkout@v4",
			Provider: "github",
		},
		// A short (non-40-hex) SHA is not a valid secure pin.
		"short sha": {
			Path:     ".github/workflows/ci.yml",
			Line:     "  - uses: actions/checkout@abc123 # v4",
			Provider: "github",
		},
		// The action-pin route is provider-gated to github.
		"pin non-github": {
			Path:     ".github/workflows/ci.yml",
			Line:     pinLine,
			Provider: "docker",
		},
		// uses: must be a real key (whitespace each side), not a substring.
		"reuses substring": {
			Path:     ".github/workflows/ci.yml",
			Line:     "      reuses: actions/checkout@" + oldSHA + " # v4.1.0",
			Provider: "github",
		},
		// A bare uses: with no reference before the @ is malformed, not a pin.
		"no reference": {
			Path:     ".github/workflows/ci.yml",
			Line:     "      - uses: @" + oldSHA + " # v4.1.0",
			Provider: "github",
		},
	}
	for name, ctx := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.IsType(t, match.Smart{}, match.For(ctx))
		})
	}
}
