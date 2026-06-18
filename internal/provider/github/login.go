package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/cli/oauth/device"
	"github.com/gechr/clover/internal/token"
)

// OAuth device-flow endpoints and the clover OAuth app client ID. The client ID
// is public - device flow needs no client secret - so it is safe to embed.
const (
	oauthClientID  = "Ov23liZM0FWwyeh6cWhp"
	deviceCodeURL  = "https://github.com/login/device/code"
	accessTokenURL = "https://github.com/login/oauth/access_token" //nolint:gosec // OAuth endpoint, not a credential
)

// oauthScopes is the access clover requests. `repo` grants read+write to private
// repositories - broader than this read-only tool needs - but it is the narrowest
// classic OAuth scope that can read private tags and releases, and device flow
// cannot issue fine-grained tokens. Public reads need no scope at all.
var oauthScopes = []string{"repo"}

// Code is the user-facing half of the device flow: the one-time code to enter
// and the URL to enter it at.
type Code struct {
	UserCode        string
	VerificationURL string
}

// Login runs the GitHub device flow and stores the minted token: it requests a
// code, hands it to prompt so the caller can show it to the user, polls until the
// user authorises in the browser, then persists the token under the GitHub host
// so the credential chain finds it. It needs no client secret and binds no local
// port, so it works headless. The context bounds the poll.
func Login(ctx context.Context, prompt func(Code)) error {
	httpClient := &http.Client{}

	code, err := device.RequestCode(httpClient, deviceCodeURL, oauthClientID, oauthScopes)
	if err != nil {
		return fmt.Errorf("github: request device code: %w", err)
	}

	prompt(Code{UserCode: code.UserCode, VerificationURL: code.VerificationURI})

	accessToken, err := device.Wait(ctx, httpClient, accessTokenURL, device.WaitOptions{
		ClientID:   oauthClientID,
		DeviceCode: code,
	})
	if err != nil {
		return fmt.Errorf("github: authorize device: %w", err)
	}
	if accessToken.Token == "" {
		return errors.New("github: device flow returned an empty token")
	}

	store, err := token.New()
	if err != nil {
		return err
	}
	if err := store.Set(host, accessToken.Token); err != nil {
		return fmt.Errorf("github: store token: %w", err)
	}
	return nil
}
