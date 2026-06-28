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
	"github.com/gechr/clover/internal/provider/gitea"
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

// cmdLogin authenticates clover with a provider, storing the minted token so
// later runs read it from the credential chain. github and gitlab use an OAuth
// device flow; gitea uses an authorization-code loopback flow (it has no device
// grant) and accepts a host and an optional client-id override.
type cmdLogin struct {
	Provider string "help:\"Provider to authenticate with\" arg:\"\" optional:\"\" clib:\"terse='Provider'\" default:\"github\" enum:\"github,gitlab,gitea\""
	Host     string "help:\"Forge host for gitea (default: codeberg.org)\" clib:\"terse='Forge host'\" placeholder:\"<host>\""
	ClientID string "help:\"OAuth client-id override for gitea\"           clib:\"terse='Client ID'\"  placeholder:\"<id>\""
}

// Help returns the detailed blurb shown in `clover login --help`.
func (c *cmdLogin) Help() string {
	return "Authenticates Clover with a provider and stores the minted token in your system keychain, so later runs read it from the credential chain instead of falling back to rate-limited anonymous access.\n\n" +
		"GitHub and GitLab use an OAuth device flow (you authorize a one-time code in the browser).\n\n" +
		"Gitea uses a browser-based loopback flow against the host given by `--host`, with an optional `--client-id`; other providers authenticate through their own credential sources."
}

// Run drives the chosen provider's interactive login, then reports success.
// gitea is the loopback authorization-code flow; the others are device flows.
func (c *cmdLogin) Run() error {
	ctx := context.Background()

	// --host and --client-id are gitea-only; github and gitlab are single-host
	// (github.com, gitlab.com). Reject them elsewhere rather than ignore them.
	if c.Provider != constant.ProviderGitea && (c.Host != "" || c.ClientID != "") {
		return fmt.Errorf("login: `--host` and `--client-id` apply only to `gitea`")
	}

	switch c.Provider {
	case constant.ProviderGitea:
		if err := gitea.Login(ctx, c.Host, c.ClientID, promptBrowser); err != nil {
			return err
		}
	default:
		login, ok := deviceLogins[c.Provider]
		if !ok {
			return fmt.Errorf("login: provider %q has no interactive login", c.Provider)
		}
		if err := login(ctx); err != nil {
			return err
		}
	}

	clog.Info().Symbol("🔑").Str(field.Provider, c.Provider).Msg("Authenticated")
	return nil
}

// promptBrowser opens the authorization URL in the browser (printing it as a
// fallback) and tells the user clover is waiting for the redirect. It writes to
// stderr so a piped stdout stays clean.
func promptBrowser(authURL string) {
	out := os.Stderr
	interactive := terminal.Is(os.Stdin) && terminal.Is(out)

	bold := func(s string) string {
		if !interactive {
			return s
		}
		return lipgloss.NewStyle().Bold(true).Render(s)
	}

	if interactive {
		fmt.Fprintf(out, "Opening your browser to authorize Clover...\n")
		if err := browser.OpenURL(authURL); err != nil {
			fmt.Fprintf(out, "Could not open a browser; visit %s\n", bold(authURL))
		}
	} else {
		fmt.Fprintf(out, "Open %s to authorize Clover\n", bold(authURL))
	}
	fmt.Fprintf(out, "Waiting for authorization...\n")
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
