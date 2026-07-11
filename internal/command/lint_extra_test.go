package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// TestRunLint drives the lint command: a clean directive passes, an unknown
// provider fails with the failures error, and the github output mode renders to
// stdout while still failing.
func TestRunLint(t *testing.T) {
	provider.Register(stubProvider{name: "lintcmd"})

	t.Run("clean directive passes", func(t *testing.T) {
		dir, _ := writeMarker(t, "# clover: provider=lintcmd repository=x/y\nversion: 1.2.0\n")
		require.NoError(t, command.RunLint([]string{dir}, nil, nil, false, resolver(), 4))
	})

	t.Run("unknown provider fails", func(t *testing.T) {
		dir, _ := writeMarker(t, "# clover: provider=lintghost repository=x/y\nversion: 1.2.0\n")
		err := command.RunLint([]string{dir}, nil, nil, false, resolver(), 4)
		require.EqualError(t, err, "1 failed")
	})

	t.Run("github output renders and fails", func(t *testing.T) {
		dir, _ := writeMarker(t, "# clover: provider=lintghost repository=x/y\nversion: 1.2.0\n")
		gh := output.GitHub
		out := captureStdout(t, func() {
			err := command.RunLint([]string{dir}, nil, &gh, false, resolver(), 4)
			require.EqualError(t, err, "1 failed")
		})
		require.NotEmpty(t, out, "github mode emits annotations to stdout")
	})

	t.Run("bad tag errors early", func(t *testing.T) {
		dir := t.TempDir()
		err := command.RunLint([]string{dir}, []string{"a,b/c"}, nil, false, resolver(), 4)
		require.Error(t, err, "a tag mixing AND and OR is rejected")
	})
}
