package command

import (
	"context"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/provider/github"
)

// loginCmd authenticates clover with a provider via its device flow, storing the
// minted token so later runs read it from the credential chain.
type loginCmd struct {
	Provider string `arg:"" optional:"" default:"github" enum:"github" help:"Provider to authenticate with" clib:"terse='Provider'"`
}

// Run drives the provider's device flow: it prints the one-time code and URL,
// then blocks until the user authorises in their browser.
func (c *loginCmd) Run() error {
	err := github.Login(context.Background(), func(code github.Code) {
		clog.Info().
			Str("code", code.UserCode).
			Str("url", code.VerificationURL).
			Msg("Open the URL and enter the code to authorize clover")
	})
	if err != nil {
		return err
	}

	clog.Info().Str("provider", constant.ProviderGithub).Msg("Authenticated")
	return nil
}
