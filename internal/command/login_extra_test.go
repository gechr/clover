package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/stretchr/testify/require"
)

// TestRunLoginNoInteractiveLogin covers the only hermetic login branch: a
// provider with no device flow is rejected before any network or OAuth.
func TestRunLoginNoInteractiveLogin(t *testing.T) {
	t.Parallel()

	require.EqualError(t, command.RunLogin("docker"),
		`login: provider "docker" has no interactive login`)
}

// TestPromptBrowser exercises the non-interactive prompt branch: with no TTY it
// prints the authorization URL and the waiting notice to stderr without opening
// a browser.
func TestPromptBrowser(t *testing.T) {
	out := captureStderr(t, func() {
		command.PromptBrowser("https://example.test/authorize")
	})
	require.Equal(t,
		"Open https://example.test/authorize to authorize Clover\nWaiting for authorization...\n",
		out,
	)
}

// TestPromptDeviceCode exercises the non-interactive device-code branch: it
// prints the one-time code and the verification URL to stderr and returns
// without blocking on stdin.
func TestPromptDeviceCode(t *testing.T) {
	out := captureStderr(t, func() {
		command.PromptDeviceCode("WXYZ-1234", "https://example.test/device")
	})
	require.Equal(t,
		"First, copy your one-time code: WXYZ-1234\n\n"+
			"Then, open https://example.test/device and enter the code to authorize Clover\n",
		out,
	)
}
