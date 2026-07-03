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
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/gechr/x/terminal"
)

// deviceLogins maps a provider name to its OAuth device flow against a host.
// docker is absent: it authenticates through its own keychain, not a Clover
// device flow.
var deviceLogins = map[string]func(
	ctx context.Context,
	host, clientID string,
	prompt func(forge.Code),
) error{
	constant.ProviderGithub: github.Login,
	constant.ProviderGitlab: gitlab.Login,
}

// cmdLogin authenticates Clover with a provider, storing the minted token so
// later runs read it from the credential chain. github and gitlab use an OAuth
// device flow; gitea uses an authorization-code loopback flow (it has no device
// grant). All three accept a `--host` for an enterprise/self-managed instance and
// an optional `--client-id` (required there, since the public host's embedded app
// is not registered on a private instance).
type cmdLogin struct {
	Provider string "help:\"Provider to authenticate with\" arg:\"\" optional:\"\" clib:\"terse='Provider'\" default:\"github\" enum:\"github,gitlab,gitea\""
	Host     string "help:\"Forge host for an enterprise/self-managed instance (default: the provider's public host)\" clib:\"terse='Forge host',group='Options/Target'\" placeholder:\"<host>\""
	ClientID string "help:\"OAuth client-id, required for an enterprise/self-managed --host\"                          clib:\"terse='Client ID',group='Options/Target'\"  placeholder:\"<id>\""
}

// Help returns the detailed blurb shown in `clover login --help`.
func (c *cmdLogin) Help() string {
	return "Authenticates Clover with a provider and stores the minted token in your system keychain, so later runs read it from the credential chain instead of falling back to rate-limited anonymous access.\n\n" +
		"GitHub and GitLab use an OAuth device flow (you authorize a one-time code in the browser); Gitea uses a browser-based loopback flow.\n\n" +
		"Pass `--host` to authenticate against a GitHub Enterprise Server, self-managed GitLab, or self-hosted Gitea instance. Such an instance runs its own OAuth app, so `--host` requires a matching `--client-id` (the public hosts use Clover's embedded app)."
}

// Run drives the chosen provider's interactive login, then reports success.
// gitea is the loopback authorization-code flow; the others are device flows.
// Each provider validates and defaults --host/--client-id itself.
func (c *cmdLogin) Run() error {
	ctx := context.Background()

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
		prompt := func(code forge.Code) {
			promptDeviceCode(code.UserCode, code.VerificationURL)
		}
		if err := login(ctx, c.Host, c.ClientID, prompt); err != nil {
			return err
		}
	}

	clog.Info().Symbol("🔑").Str(field.Provider, c.Provider).Msg("Authenticated")
	return nil
}

// promptBrowser opens the authorization URL in the browser (printing it as a
// fallback) and tells the user Clover is waiting for the redirect. It writes to
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
			fmt.Fprintf(out, "Could not open a browser - visit %s\n", bold(authURL))
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
		fmt.Fprintf(out, "Then, open %s and enter the code to authorize Clover\n", verificationURL)
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
			"Could not open a browser - go to %s and enter the code\n",
			verificationURL,
		)
	}
}
