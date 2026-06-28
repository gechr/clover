package command

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/cli/browser"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/gechr/x/terminal"
)

// deviceLogins maps a provider name to its OAuth device flow. Each provider keeps
// its own typed Code, so the dispatch adapts it to promptDeviceCode's plain
// arguments here rather than forcing a shared type across the providers. docker
// is absent: it authenticates through its own keychain, not a clover device flow.
var deviceLogins = map[string]func(context.Context) error{
	constant.ProviderGithub: func(ctx context.Context) error {
		return github.Login(ctx, func(c github.Code) {
			promptDeviceCode(c.UserCode, c.VerificationURL)
		})
	},
	constant.ProviderGitlab: func(ctx context.Context) error {
		return gitlab.Login(ctx, func(c gitlab.Code) {
			promptDeviceCode(c.UserCode, c.VerificationURL)
		})
	},
}

// cmdLogin authenticates clover with a provider via its device flow, storing the
// minted token so later runs read it from the credential chain. The enum bounds
// the providers to those wired in deviceLogins.
type cmdLogin struct {
	Provider string "help:\"Provider to authenticate with (default: `github`)\" arg:\"\" optional:\"\" clib:\"terse='Provider'\" default:\"github\" enum:\"github,gitlab\""
}

// Help returns the detailed blurb shown in `clover login --help`.
func (c *cmdLogin) Help() string {
	return "Authenticates Clover with a provider through its OAuth device flow: you authorize a one-time code in the browser, and the minted token is stored in your system keychain so later runs read it from the credential chain instead of falling back to rate-limited anonymous access.\n\n" +
		"GitHub and GitLab support an interactive login; other providers authenticate through their own credential sources."
}

// Run drives the chosen provider's device flow, then reports success.
func (c *cmdLogin) Run() error {
	login, ok := deviceLogins[c.Provider]
	if !ok {
		return fmt.Errorf("login: provider %q has no interactive login", c.Provider)
	}
	if err := login(context.Background()); err != nil {
		return err
	}

	clog.Info().Symbol("🔑").Str(field.Provider, c.Provider).Msg("Authenticated")
	return nil
}

// promptDeviceCode shows the one-time code (copying it to the clipboard when it
// can) and, on a terminal, opens the verification URL in the browser after the
// user presses Enter. Off a terminal it just prints the URL to enter manually.
// It writes to stderr so a piped stdout stays clean.
func promptDeviceCode(userCode, verificationURL string) {
	out := os.Stderr
	interactive := terminal.Is(os.Stdin) && terminal.Is(out)

	// bold emphasises the code and the key to press, but only on a terminal: a
	// piped or redirected stream gets plain text rather than escape sequences.
	bold := func(s string) string {
		if !interactive {
			return s
		}
		return lipgloss.NewStyle().Bold(true).Render(s)
	}

	// Copy the code so the user can paste it, but say nothing about it - the
	// instruction to copy reads the same whether or not the clipboard was available.
	_ = clipboard.WriteAll(userCode)
	fmt.Fprintf(out, "First, copy your one-time code: %s\n\n", bold(userCode))

	if !interactive {
		fmt.Fprintf(out, "Then, open %s and enter the code to authorize clover\n", verificationURL)
		return
	}

	fmt.Fprintf(
		out,
		"Then, press %s to open %s in your browser... ",
		bold("Enter"),
		verificationURL,
	)
	_ = bufio.NewScanner(os.Stdin).Scan()
	if err := browser.OpenURL(verificationURL); err != nil {
		fmt.Fprintf(
			out,
			"Could not open a browser; go to %s and enter the code\n",
			verificationURL,
		)
	}
}
