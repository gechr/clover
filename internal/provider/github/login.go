package github

import (
	"context"
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

// oauthScopes is the access clover requests. repo covers reading tags and
// releases of private repositories; public reads need no scope at all.
var oauthScopes = []string{"repo"}

// Code is the user-facing half of the device flow: the one-time code to enter
// and the URL to enter it at.
type Code struct {
	UserCode        string
	VerificationURL string
}

// Login runs the GitHub device flow and stores the minted token: it requests a
// code, hands it to display so the caller can prompt the user, polls until the
// user authorises in the browser, then persists the token under the GitHub host
// so the credential chain finds it. It needs no client secret and binds no local
// port, so it works headless. The context bounds the poll.
func Login(ctx context.Context, display func(Code)) error {
	httpClient := &http.Client{}

	code, err := device.RequestCode(httpClient, deviceCodeURL, oauthClientID, oauthScopes)
	if err != nil {
		return fmt.Errorf("github: request device code: %w", err)
	}

	display(Code{UserCode: code.UserCode, VerificationURL: code.VerificationURI})

	accessToken, err := device.Wait(ctx, httpClient, accessTokenURL, device.WaitOptions{
		ClientID:   oauthClientID,
		DeviceCode: code,
	})
	if err != nil {
		return fmt.Errorf("github: authorize device: %w", err)
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
