package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/cli/oauth/device"
	"github.com/gechr/clover/internal/token"
)

// OAuth device-flow endpoints and the clover OAuth application's client ID.
//
// oauthClientID is the public Application ID of the "clover" OAuth application
// registered on gitlab.com (User Settings > Applications, Device authorization
// grant enabled, read_api scope). Device flow is a public client - no client
// secret - so the ID is safe to embed. It is gitlab.com-specific: a self-managed
// instance has its own application and ID.
const (
	oauthClientID  = "44645c75a26f9ad0817f292aef95996b65618d2343e4e0a04a549df4ae75f4f4"
	deviceCodeURL  = "https://gitlab.com/oauth/authorize_device"
	accessTokenURL = "https://gitlab.com/oauth/token" //nolint:gosec // OAuth endpoint, not a credential
)

// oauthScopes is the access clover requests: read-only API, enough to read a
// private project's tags and releases and nothing more.
var oauthScopes = []string{"read_api"}

// Code is the user-facing half of the device flow: the one-time code to enter
// and the URL to enter it at.
type Code struct {
	UserCode        string
	VerificationURL string
}

// Login runs the GitLab device flow and stores the minted token: it requests a
// code, hands it to prompt so the caller can show it to the user, polls until the
// user authorises in the browser, then persists the token under the GitLab host
// so the credential chain finds it. It needs no client secret and binds no local
// port, so it works headless. The context bounds the poll.
func Login(ctx context.Context, prompt func(Code)) error {
	httpClient := &http.Client{}

	code, err := device.RequestCode(httpClient, deviceCodeURL, oauthClientID, oauthScopes)
	if err != nil {
		return fmt.Errorf("gitlab: request device code: %w", err)
	}

	prompt(Code{UserCode: code.UserCode, VerificationURL: code.VerificationURI})

	accessToken, err := device.Wait(ctx, httpClient, accessTokenURL, device.WaitOptions{
		ClientID:   oauthClientID,
		DeviceCode: code,
	})
	if err != nil {
		return fmt.Errorf("gitlab: authorize device: %w", err)
	}
	if accessToken.Token == "" {
		return errors.New("gitlab: device flow returned an empty token")
	}

	store, err := token.New()
	if err != nil {
		return err
	}
	if err := store.Set(defaultHost, accessToken.Token); err != nil {
		return fmt.Errorf("gitlab: store token: %w", err)
	}
	return nil
}
