package gitlab

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/cli/oauth/device"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/token"
)

// oauthClientID is the public Application ID of the "clover" OAuth application
// registered on gitlab.com (User Settings > Applications, Device authorization
// grant enabled, read_api scope). Device flow is a public client - no client
// secret - so the ID is safe to embed. It is gitlab.com-specific: a self-managed
// instance has its own application and ID, supplied via --client-id.
const oauthClientID = "44645c75a26f9ad0817f292aef95996b65618d2343e4e0a04a549df4ae75f4f4"

// oauthScopes is the access clover requests: read-only API, enough to read a
// private project's tags and releases and nothing more.
var oauthScopes = []string{"read_api"}

// deviceCodeURL and accessTokenURL are GitLab's device-flow endpoints on a host.
// A self-managed instance serves the same paths under its own origin.
func deviceCodeURL(host string) string {
	return "https://" + host + "/oauth/authorize_device"
}

func accessTokenURL(host string) string {
	return "https://" + host + "/oauth/token"
}

// Code is the user-facing half of the device flow: the one-time code to enter
// and the URL to enter it at.
type Code struct {
	UserCode        string
	VerificationURL string
}

// Login runs the GitLab device flow against host and stores the minted token: it
// requests a code, hands it to prompt so the caller can show it to the user,
// polls until the user authorises in the browser, then persists the token under
// the host so the credential chain finds it. It needs no client secret and binds
// no local port, so it works headless. host and clientID default to gitlab.com
// and the embedded clover app; a self-managed host has no embeddable app, so it
// requires an explicit --client-id. The context bounds the poll.
func Login(ctx context.Context, host, clientID string, prompt func(Code)) error {
	host, ok := forge.NormalizeHost(cmp.Or(host, defaultHost))
	if !ok {
		return errors.New("gitlab: invalid host")
	}
	if host == defaultHost {
		clientID = cmp.Or(clientID, oauthClientID)
	}
	if clientID == "" {
		return fmt.Errorf(
			"gitlab: %s requires --client-id (register an application on the instance)",
			host,
		)
	}

	httpClient := &http.Client{}

	code, err := device.RequestCode(httpClient, deviceCodeURL(host), clientID, oauthScopes)
	if err != nil {
		return fmt.Errorf("gitlab: request device code: %w", err)
	}

	prompt(Code{UserCode: code.UserCode, VerificationURL: code.VerificationURI})

	accessToken, err := device.Wait(ctx, httpClient, accessTokenURL(host), device.WaitOptions{
		ClientID:   clientID,
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
	if err := store.Set(host, accessToken.Token); err != nil {
		return fmt.Errorf("gitlab: store token: %w", err)
	}
	return nil
}
