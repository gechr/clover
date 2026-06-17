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
	require.Equal(t, "v4.1.0", located.Raw)

	got, changed, err := match.NewActionPin().Render(line, located, model.Candidate{
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

	got, _, err := match.NewActionPin().
		Render(line, located, model.Candidate{Version: "1.1.0", Commit: newSHA})
	require.NoError(t, err)
	require.Equal(t, `  - uses: "owner/repo/path@`+newSHA+`" # v1.1.0`, got)
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
		_, _, err := match.NewActionPin().
			Render(line, located, model.Candidate{Version: "4.2.0", Commit: bad})
		require.Error(t, err, "commit %q must be rejected", bad)
	}
}

func TestForRoutesWorkflowToActionPin(t *testing.T) {
	t.Parallel()

	rw := match.For(match.Context{
		Path:     ".github/workflows/ci.yml",
		Line:     "  - uses: actions/checkout@" + oldSHA + " # v4.1.0",
		Provider: "github",
	})
	require.IsType(t, match.ActionPin{}, rw)
}

func TestForLeavesNonWorkflowToSmart(t *testing.T) {
	t.Parallel()

	rw := match.For(match.Context{
		Path:     "Dockerfile",
		Line:     "FROM nginx:1.27.0",
		Provider: "github",
	})
	require.IsType(t, match.Smart{}, rw)
}
