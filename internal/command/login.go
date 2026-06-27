package command

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/atotto/clipboard"
	"github.com/cli/browser"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/gechr/x/terminal"
)

// cmdLogin authenticates clover with a provider via its device flow, storing the
// minted token so later runs read it from the credential chain.
//
// github is the only provider with an interactive login, so the command calls
// github.Login directly rather than dispatching through a provider capability
// interface; the enum keeps that scope explicit. A Login capability (mirroring
// Authenticator) would be premature with one implementer - docker authenticates
// through its own keychain, not a clover device flow - so revisit it only when a
// second provider needs interactive auth.
type cmdLogin struct {
	Provider string "help:\"Provider to authenticate with (default: `github`)\" arg:\"\" optional:\"\" clib:\"terse='Provider'\" default:\"github\" enum:\"github\""
}

// Help returns the detailed blurb shown in `clover login --help`.
func (c *cmdLogin) Help() string {
	return "Authenticates Clover with a provider through its OAuth device flow: you authorize a one-time code in the browser, and the minted token is stored in your system keychain so later runs read it from the credential chain instead of falling back to rate-limited anonymous access.\n\n" +
		"GitHub is currently the default (and only) provider that supports an interactive login; other providers authenticate through their own credential sources."
}

// Run drives the provider's device flow, then reports success.
func (c *cmdLogin) Run() error {
	if err := github.Login(context.Background(), promptDeviceCode); err != nil {
		return err
	}

	clog.Info().Symbol("🔑").Str(field.Provider, constant.ProviderGithub).Msg("Authenticated")
	return nil
}

// promptDeviceCode shows the one-time code (copying it to the clipboard when it
// can) and, on a terminal, opens the verification URL in the browser after the
// user presses Enter. Off a terminal it just prints the URL to enter manually.
// It writes to stderr so a piped stdout stays clean.
func promptDeviceCode(code github.Code) {
	out := os.Stderr

	if clipboard.WriteAll(code.UserCode) == nil {
		fmt.Fprintf(out, "Copied your one-time code to the clipboard: %s\n", code.UserCode)
	} else {
		fmt.Fprintf(out, "Copy your one-time code: %s\n", code.UserCode)
	}

	if !terminal.Is(os.Stdin) || !terminal.Is(out) {
		fmt.Fprintf(out, "Open %s and enter the code to authorize clover\n", code.VerificationURL)
		return
	}

	fmt.Fprintf(out, "Press Enter to open %s in your browser... ", code.VerificationURL)
	_ = bufio.NewScanner(os.Stdin).Scan()
	if err := browser.OpenURL(code.VerificationURL); err != nil {
		fmt.Fprintf(
			out,
			"Could not open a browser; go to %s and enter the code\n",
			code.VerificationURL,
		)
	}
}
