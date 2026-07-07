package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

// TestActionTagConvertsToSecurePin covers the core conversion: a tag pin is
// rewritten to the secure pin format, the tag replaced by the commit SHA and
// the resolved version appended as the comment.
func TestActionTagConvertsToSecurePin(t *testing.T) {
	t.Parallel()

	line := "  - uses: actions/checkout@v4.1.0"

	located, err := match.NewActionTag().Locate(line)
	require.NoError(t, err)
	require.Equal(t, "v4.1.0", located.Current())
	require.NotNil(t, located.Semver())

	got, changed, err := located.Render(line, model.Candidate{
		Version: "v4.2.0",
		Commit:  newSHA,
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "  - uses: actions/checkout@"+newSHA+" # v4.2.0", got)
}

// TestActionTagCommentCarriesFullPrecision confirms the appended comment names
// the full resolved version even when the original tag was abbreviated - the
// comment documents what the SHA points at, not the old tag's style.
func TestActionTagCommentCarriesFullPrecision(t *testing.T) {
	t.Parallel()

	line := "  - uses: actions/checkout@v4"

	located, err := match.NewActionTag().Locate(line)
	require.NoError(t, err)
	require.Equal(t, "v4", located.Current())

	got, changed, err := located.Render(line, model.Candidate{
		Version: "4.2.2",
		Commit:  newSHA,
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "  - uses: actions/checkout@"+newSHA+" # v4.2.2", got)
}

// TestActionTagQuoted covers a quoted reference: the closing quote after the
// tag is allowed, and the comment lands after it.
func TestActionTagQuoted(t *testing.T) {
	t.Parallel()

	line := `  - uses: "owner/repo/path@1.0.0"`

	located, err := match.NewActionTag().Locate(line)
	require.NoError(t, err)

	got, _, err := located.Render(line, model.Candidate{Version: "1.1.0", Commit: newSHA})
	require.NoError(t, err)
	require.Equal(t, `  - uses: "owner/repo/path@`+newSHA+`" # v1.1.0`, got)
}

func TestActionTagRendered(t *testing.T) {
	t.Parallel()

	line := "  - uses: actions/checkout@v4"
	located, err := match.NewActionTag().Locate(line)
	require.NoError(t, err)

	r, ok := located.(match.Renderer)
	require.True(t, ok, "action tag must report its rendered value")
	require.Equal(t, "v7.0.1", r.Rendered(model.Candidate{Version: "7.0.1", Commit: newSHA}))
}

func TestActionTagLocateErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"no uses":          "FROM nginx:1.27",
		"local action":     "  - uses: ./.github/actions/build",
		"unpinned":         "  - uses: actions/checkout",
		"branch ref":       "  - uses: actions/checkout@main",
		"empty tag":        "  - uses: actions/checkout@",
		"trailing comment": "  - uses: actions/checkout@v4 # note",
		"trailing garbage": "  - uses: actions/checkout@v4 extra",
	}

	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := match.NewActionTag().Locate(line)
			require.Error(t, err)
		})
	}
}

func TestActionTagRenderRequiresFullCommit(t *testing.T) {
	t.Parallel()

	line := "  - uses: actions/checkout@v4"
	located, err := match.NewActionTag().Locate(line)
	require.NoError(t, err)

	_, _, err = located.Render(line, model.Candidate{Version: "4.2.0", Commit: "abc123"})
	require.Error(t, err, "a short commit must be rejected")
}

// TestForRoutesTagPinToActionTag confirms a tag-pinned uses: routes to the
// action-tag rewriter, which converts it to the secure pin format.
func TestForRoutesTagPinToActionTag(t *testing.T) {
	t.Parallel()

	rw := match.For(match.Context{
		Path:     ".github/workflows/ci.yml",
		Line:     "  - uses: actions/checkout@v4",
		Provider: "github",
	})
	require.IsType(t, match.ActionTag{}, rw)
}
