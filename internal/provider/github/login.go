package github

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

// oauthClientID is the public Application (client) ID of the clover OAuth app on
// github.com. The client ID is public - device flow needs no client secret - so
// it is safe to embed. It is github.com-specific: a GitHub Enterprise Server
// instance has its own per-instance app, supplied via --client-id.
const oauthClientID = "Ov23liZM0FWwyeh6cWhp"

// oauthScopes is the access clover requests. `repo` grants read+write to private
// repositories - broader than this read-only tool needs - but it is the narrowest
// classic OAuth scope that can read private tags and releases, and device flow
// cannot issue fine-grained tokens. Public reads need no scope at all.
var oauthScopes = []string{"repo"}

// deviceCodeURL and accessTokenURL are GitHub's device-flow endpoints on a host.
// github.com serves them on github.com itself (not api.github.com); a GHES host
// serves the same paths under its own origin.
func deviceCodeURL(host string) string {
	return "https://" + host + "/login/device/code"
}

func accessTokenURL(host string) string {
	return "https://" + host + "/login/oauth/access_token"
}

// Code is the user-facing half of the device flow: the one-time code to enter
// and the URL to enter it at.
type Code struct {
	UserCode        string
	VerificationURL string
}

// Login runs the GitHub device flow against host and stores the minted token: it
// requests a code, hands it to prompt so the caller can show it to the user,
// polls until the user authorises in the browser, then persists the token under
// the host so the credential chain finds it. It needs no client secret and binds
// no local port, so it works headless. host and clientID default to github.com
// and the embedded clover app; a GitHub Enterprise Server host has no embeddable
// app, so it requires an explicit --client-id. The context bounds the poll.
func Login(ctx context.Context, host, clientID string, prompt func(Code)) error {
	host, ok := forge.NormalizeHost(cmp.Or(host, defaultHost))
	if !ok {
		return errors.New("github: invalid host")
	}
	if host == defaultHost {
		clientID = cmp.Or(clientID, oauthClientID)
	}
	if clientID == "" {
		return fmt.Errorf(
			"github: %s requires --client-id (register an OAuth app on the instance)",
			host,
		)
	}

	httpClient := &http.Client{}

	code, err := device.RequestCode(httpClient, deviceCodeURL(host), clientID, oauthScopes)
	if err != nil {
		return fmt.Errorf("github: request device code: %w", err)
	}

	prompt(Code{UserCode: code.UserCode, VerificationURL: code.VerificationURI})

	accessToken, err := device.Wait(ctx, httpClient, accessTokenURL(host), device.WaitOptions{
		ClientID:   clientID,
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
